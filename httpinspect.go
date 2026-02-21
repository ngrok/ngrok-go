package ngrok

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"time"

	"golang.ngrok.com/ngrok/v2/internal/httpx"
)

// httpServe uses httputil.ReverseProxy to forward HTTP traffic from the proxy
// connection to the upstream backend.
//
// It creates an http.Server with a handler that wraps ReverseProxy and a
// statusCaptureWriter for event emission, then uses httpx.ServeConnServer
// to serve the single proxy connection without needing a real net.Listener.
func (e *endpointForwarder) httpServe(proxyConn net.Conn) {
	target := e.upstreamURL
	transport := e.buildHTTPTransport()

	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(&target)
			// Preserve the original Host header from the inbound request
			pr.Out.Host = pr.In.Host
		},
		Transport: transport,
	}

	// handler wraps the ReverseProxy to capture per-request metrics.
	// We use a statusCaptureWriter to intercept the status code written
	// by ReverseProxy so we can emit EventHTTPRequestComplete with the
	// method, path, status, and duration of each request/response cycle.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusCaptureWriter{ResponseWriter: w}
		rp.ServeHTTP(sw, r)
		e.emitConnectionEvent(newHTTPRequestComplete(
			e, r.Method, r.URL.RequestURI(), sw.statusCode, time.Since(start),
		))
	})

	server := &http.Server{
		Handler: handler,
	}

	srv := httpx.NewServeConnServer(server, slog.Default())

	go srv.ListenAndServe() //nolint:errcheck

	srv.ServeConn(context.Background(), proxyConn, nil) //nolint:errcheck
}

// buildHTTPTransport creates an http.Transport configured with the
// endpoint's upstream settings
func (e *endpointForwarder) buildHTTPTransport() *http.Transport {
	tlsConfig := &tls.Config{
		ServerName: e.upstreamURL.Hostname(),
	}
	if e.upstreamTLSClientConfig != nil {
		tlsConfig = e.upstreamTLSClientConfig.Clone()
		if tlsConfig.ServerName == "" {
			tlsConfig.ServerName = e.upstreamURL.Hostname()
		}
	}

	transport := &http.Transport{
		TLSClientConfig:   tlsConfig,
		ForceAttemptHTTP2: e.upstreamProtocol == "http2",
	}

	if e.upstreamDialer != nil {
		transport.DialContext = e.upstreamDialer.DialContext
	}

	return transport
}

// statusCaptureWriter wraps an http.ResponseWriter to capture the status code
// for event emission. Unwrap() is provided so that http.ResponseController
// can discover optional interfaces (Flusher, Hijacker, etc.) on the
// underlying ResponseWriter.
type statusCaptureWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (w *statusCaptureWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.statusCode = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusCaptureWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.statusCode = http.StatusOK
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap returns the underlying ResponseWriter for interface detection.
func (w *statusCaptureWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
