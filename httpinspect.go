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

// httpStack is an endpoint's HTTP forwarding machinery: one http.Server that
// serves every inbound edge connection, and one http.Transport that pools
// connections to the upstream.
//
// There is deliberately one of these per endpoint rather than one per inbound
// connection. http.Transport is documented as something to reuse rather than
// create as needed - it is safe for concurrent use and it is what holds the
// upstream connection pool, so building one per connection both leaks sockets
// and forfeits keep-alive entirely, making every inbound connection pay a
// fresh TCP and TLS handshake to the upstream. httpx.ServeConnServer exists
// precisely so a single http.Server can serve individually-accepted
// connections (see golang/go#36673), so it is built to be shared this way.
type httpStack struct {
	srv       *httpx.ServeConnServer
	server    *http.Server
	transport *http.Transport
}

// httpStackForConn returns the endpoint's HTTP stack, building it on first use.
// It returns nil if the endpoint has already been torn down.
//
// This is lazy rather than built in start() because an endpoint only needs an
// HTTP stack if it actually forwards HTTP, and UpdateUpstream can switch an
// endpoint between HTTP and raw TCP upstreams at any point.
func (e *endpointForwarder) httpStackForConn(ctx context.Context) *httpStack {
	e.httpMu.Lock()
	defer e.httpMu.Unlock()

	if e.httpClosed {
		return nil
	}
	if e.httpStack == nil {
		e.httpStack = e.newHTTPStack(ctx)
	}
	return e.httpStack
}

// closeHTTPStack tears down the endpoint's HTTP stack. It is called once, when
// the endpoint's forward loop exits.
func (e *endpointForwarder) closeHTTPStack() {
	e.httpMu.Lock()
	stack := e.httpStack
	e.httpStack = nil
	e.httpClosed = true
	e.httpMu.Unlock()

	if stack == nil {
		return
	}
	stack.server.Close() //nolint:errcheck
	stack.transport.CloseIdleConnections()
}

// newHTTPStack builds the endpoint's HTTP server, reverse proxy and upstream
// transport. Everything it creates is shared by every inbound connection, so
// nothing here may capture per-connection state.
func (e *endpointForwarder) newHTTPStack(ctx context.Context) *httpStack {
	transport := e.buildHTTPTransport()

	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			// Read the upstream per request so that UpdateUpstream takes effect
			// without having to rebuild the stack.
			pr.SetURL(e.upstreamURL.Load())
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
		Handler:           handler,
		ReadHeaderTimeout: 30 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	srv := httpx.NewServeConnServer(server, slog.Default())
	go srv.ListenAndServe() //nolint:errcheck

	return &httpStack{srv: srv, server: server, transport: transport}
}

// httpServe forwards a single inbound edge connection to the upstream using the
// endpoint's HTTP stack. It blocks until that connection is finished.
func (e *endpointForwarder) httpServe(ctx context.Context, proxyConn net.Conn) {
	stack := e.httpStackForConn(ctx)
	if stack == nil {
		// The endpoint is shutting down and will not serve this connection.
		proxyConn.Close() //nolint:errcheck
		return
	}

	stack.srv.ServeConn(ctx, proxyConn, nil) //nolint:errcheck
}

// buildHTTPTransport creates an http.Transport configured with the
// endpoint's upstream settings
func (e *endpointForwarder) buildHTTPTransport() *http.Transport {
	// ServerName is deliberately left unset. http.Transport derives it from
	// each request's URL, so one transport keeps sending the right SNI even
	// after UpdateUpstream changes the upstream hostname. An explicit
	// ServerName here would pin the transport to whichever upstream happened to
	// be configured when it was built.
	//
	// The config has to stay non-nil even when the caller supplied none: a nil
	// TLSClientConfig makes net/http enable automatic HTTP/2 negotiation for
	// https upstreams, which is not what this endpoint asked for.
	tlsConfig := &tls.Config{} //nolint:gosec // ServerName is filled in per request
	if e.upstreamTLSClientConfig != nil {
		tlsConfig = e.upstreamTLSClientConfig.Clone()
	}

	transport := &http.Transport{
		TLSClientConfig:   tlsConfig,
		ForceAttemptHTTP2: e.upstreamProtocol == "http2",
		// A hand-built Transport leaves this at zero, which net/http reads as
		// "no limit" - unlike http.DefaultTransport, which uses 90s.
		IdleConnTimeout: 90 * time.Second,
		// The stdlib default is 2, which is far too low for something whose
		// whole job is forwarding to a single upstream.
		MaxIdleConnsPerHost: 100,
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
