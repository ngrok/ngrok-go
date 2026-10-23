package ngrok

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/http/httputil"
	"time"

	"golang.ngrok.com/ngrok/v2/internal/httpx"
)

// httpReadHeaderTimeout limits how long httpServe waits for each request's
// headers. Tests shorten it.
var httpReadHeaderTimeout = 3 * time.Second

// forwardingHeaders are the headers that httputil.ReverseProxy removes from
// the outbound request when Rewrite is set.
var forwardingHeaders = []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"}

// httpServe uses httputil.ReverseProxy to forward HTTP traffic from the proxy
// connection to the upstream backend.
//
// It creates an http.Server with a handler that wraps ReverseProxy and a
// statusCaptureWriter for event emission, then uses httpx.ServeConnServer
// to serve the single proxy connection without needing a real net.Listener.
func (e *endpointForwarder) httpServe(proxyConn net.Conn) {
	target := e.upstreamURL.Load()
	transport := e.buildHTTPTransport()
	// This transport is built per inbound connection, so it has to be torn down
	// per inbound connection too. Nothing else ever closes it: net/http sets no
	// finalizer on Transport, and an idle upstream connection keeps its read
	// loop goroutine alive, so an abandoned transport holds its socket open
	// until the upstream gives up. Deferred so it runs after server.Close().
	defer transport.CloseIdleConnections()

	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out = pr.Out.WithContext(e.traceUpstreamTLS(pr.Out.Context()))
			// Preserve the original Host header from the inbound request
			pr.Out.Host = pr.In.Host
			// ReverseProxy removes the forwarding headers when Rewrite is set.
			// The ngrok edge sets them, so restore them as received. Do not use
			// SetXForwarded: it adds the edge connection's address, not the
			// client's.
			for _, h := range forwardingHeaders {
				if v, ok := pr.In.Header[h]; ok {
					pr.Out.Header[h] = v
				}
			}
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
		// Without a limit, a client that never completes its request headers
		// holds the connection and this goroutine until the edge closes it.
		ReadHeaderTimeout: httpReadHeaderTimeout,
		Handler:           handler,
	}

	srv := httpx.NewServeConnServer(server, slog.Default())

	go srv.ListenAndServe() //nolint:errcheck

	srv.ServeConn(context.Background(), proxyConn, nil) //nolint:errcheck
	server.Close()                                      //nolint:errcheck
}

// buildHTTPTransport creates an http.Transport configured with the
// endpoint's upstream settings
func (e *endpointForwarder) buildHTTPTransport() *http.Transport {
	u := e.upstreamURL.Load()
	tlsConfig := &tls.Config{
		ServerName: u.Hostname(),
	}
	if e.upstreamTLSClientConfig != nil {
		tlsConfig = e.upstreamTLSClientConfig.Clone()
		if tlsConfig.ServerName == "" {
			tlsConfig.ServerName = u.Hostname()
		}
	}

	transport := &http.Transport{
		TLSClientConfig:   tlsConfig,
		ForceAttemptHTTP2: e.upstreamProtocol == "http2",
		// A hand-built Transport leaves this at zero, which net/http reads as
		// "no limit" - unlike http.DefaultTransport, which uses 90s. Without
		// it, an idle upstream socket survives for as long as the inbound
		// connection that created it, which the ngrok edge may hold open for a
		// long time.
		IdleConnTimeout: 90 * time.Second,
	}

	// A zero net.Dialer dials the same way as a Transport with no DialContext.
	dialer := e.upstreamDialer
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		start := time.Now()
		conn, err := dialer.DialContext(ctx, network, address)
		e.emitConnectionEvent(newUpstreamDialComplete(e, time.Since(start), err))
		return conn, err
	}

	return transport
}

// traceUpstreamTLS returns ctx with a trace that emits an
// EventUpstreamTLSHandshakeComplete for each handshake that the Transport runs. The
// Transport keeps the handshake because it picks the ALPN protocols for each
// connection. With a DialTLSContext, every connection offers the same
// protocols, and an HTTP/1.1 upgrade fails when the upstream protocol is
// http2. The trace stays in the dial context after the request is canceled,
// so an abandoned handshake still reports.
func (e *endpointForwarder) traceUpstreamTLS(ctx context.Context) context.Context {
	var start time.Time
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		TLSHandshakeStart: func() { start = time.Now() },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			e.emitConnectionEvent(newUpstreamTLSHandshakeComplete(e, time.Since(start), err))
		},
	})
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
