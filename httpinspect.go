package ngrok

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"sync"
	"time"
)

// httpServe uses httputil.ReverseProxy to forward HTTP traffic from the proxy
// connection to the upstream backend
func (e *endpointForwarder) httpServe(proxyConn net.Conn) {
	listener := newSingleConnListener(proxyConn)

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

	// server uses http.Server to parse HTTP off the raw net.Conn and
	// provide the (ResponseWriter, *Request) pair that ReverseProxy needs.
	// ConnState closes the listener when the connection finishes, which
	// unblocks the second Accept() call and lets Serve() return.
	server := &http.Server{
		Handler: handler,
		ConnState: func(_ net.Conn, state http.ConnState) {
			if state == http.StateClosed || state == http.StateHijacked {
				listener.Close() //nolint:errcheck
			}
		},
	}

	server.Serve(listener) //nolint:errcheck
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

// singleConnListener wraps a single net.Conn as a net.Listener.
// This is needed because http.Server.Serve() only accepts a net.Listener,
// but we already have an established connection (from ngrok's muxado session)
// rather than a port to listen on. This adapter feeds that one connection
// to Serve(), then blocks until the connection is done before letting
// Serve() exit.
type singleConnListener struct {
	conn    net.Conn
	once    sync.Once
	closeCh chan struct{}
}

func newSingleConnListener(conn net.Conn) *singleConnListener {
	return &singleConnListener{
		conn:    conn,
		closeCh: make(chan struct{}),
	}
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	var accepted bool
	l.once.Do(func() { accepted = true })
	if accepted {
		return l.conn, nil
	}
	// Block until the connection is closed
	<-l.closeCh
	return nil, net.ErrClosed
}

func (l *singleConnListener) Close() error {
	select {
	case <-l.closeCh:
		// already closed
	default:
		close(l.closeCh)
	}
	return nil
}

func (l *singleConnListener) Addr() net.Addr {
	return l.conn.LocalAddr()
}

// statusCaptureWriter wraps an http.ResponseWriter to capture the status code
// for event emission. The Hijack and Flusher functions are preserved
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

func (w *statusCaptureWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("upstream ResponseWriter does not implement http.Hijacker")
	}
	return hj.Hijack()
}

func (w *statusCaptureWriter) Flush() {
	fl, ok := w.ResponseWriter.(http.Flusher)
	if ok {
		fl.Flush()
	}
	// todo: maybe log an error?
}

// Unwrap returns the underlying ResponseWriter for interface detection.
func (w *statusCaptureWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
