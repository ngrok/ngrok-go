package ngrok

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// EndpointForwarder is an Endpoint that forwards traffic to an upstream service.
type EndpointForwarder struct {
	baseEndpoint
	listener                *EndpointListener
	upstreamURL             url.URL
	upstreamTLSClientConfig *tls.Config
	upstreamProtocol        string
	proxyProtocol           ProxyProtoVersion
	upstreamDialer          Dialer
}

// Start begins forwarding connections from the listener to the upstream URL
func (e *EndpointForwarder) start(ctx context.Context) {
	go e.forwardLoop(ctx)
}

// forwardLoop is the main loop that forwards connections
func (e *EndpointForwarder) forwardLoop(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			// Context cancelled, exit the loop
			return
		default:
			// Accept connection with TLS termination already handled by the listener
			conn, err := e.listener.Accept()
			if err != nil {
				// Signal done if accept fails
				e.signalDone()
				return
			}

			// Handle the connection in a goroutine
			go func() {
				e.handleConnection(ctx, conn)
			}()
		}
	}
}

// handleConnection processes a single connection
func (e *EndpointForwarder) handleConnection(ctx context.Context, conn net.Conn) {
	start := time.Now()
	remoteAddr := conn.RemoteAddr().String()

	e.emitConnectionEvent(newConnectionOpened(e, remoteAddr))

	proxyConn := &countingConn{Conn: conn}

	if e.isHTTP() {
		e.httpServe(proxyConn)
		e.emitConnectionEvent(newConnectionClosed(e, remoteAddr, time.Since(start), proxyConn.bytesRead.Load(), proxyConn.bytesWritten.Load()))
	} else {
		backend, err := e.connectToBackend(ctx)
		if err != nil {
			conn.Close() //nolint:errcheck
			e.emitConnectionEvent(newConnectionClosed(e, remoteAddr, time.Since(start), 0, 0))
			return
		}
		backendConn := &countingConn{Conn: backend}
		e.join(proxyConn, backendConn)
		e.emitConnectionEvent(newConnectionClosed(e, remoteAddr, time.Since(start), proxyConn.bytesRead.Load(), backendConn.bytesRead.Load()))
	}
}

func (e *EndpointForwarder) emitConnectionEvent(evt Event) {
	e.agent.emitEvent(evt)
}

func (e *EndpointForwarder) isHTTP() bool {
	switch strings.ToLower(e.upstreamURL.Scheme) {
	case "http", "https":
		return true
	default:
		return false
	}
}

type countingConn struct {
	net.Conn
	bytesRead    atomic.Int64
	bytesWritten atomic.Int64
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.bytesRead.Add(int64(n))
	return n, err
}

func (c *countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.bytesWritten.Add(int64(n))
	return n, err
}

// connectToBackend establishes a connection to the upstream URL
func (e *EndpointForwarder) connectToBackend(ctx context.Context) (net.Conn, error) {
	// Parse host and port from URL
	host := e.upstreamURL.Hostname()
	port := e.upstreamURL.Port()
	if port == "" {
		// Default ports based on scheme
		switch {
		case usesTLS(e.upstreamURL.Scheme):
			port = "443"
		case strings.ToLower(e.upstreamURL.Scheme) == "http":
			port = "80"
		default:
			port = "80" // Default fallback
		}
	}
	if host == "" {
		host = "localhost"
	}

	// Connect to the backend
	address := net.JoinHostPort(host, port)

	// Use custom dialer if provided, otherwise use default dialer
	dialer := e.upstreamDialer
	if dialer == nil {
		dialer = &net.Dialer{
			Timeout: 3 * time.Second,
		}
	}

	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}

	// For HTTPS/TLS upstreams, establish TLS
	if usesTLS(e.upstreamURL.Scheme) {
		config := &tls.Config{
			ServerName: e.upstreamURL.Hostname(),
		}

		// Use custom TLS client config if provided
		if e.upstreamTLSClientConfig != nil {
			// Use the provided config as a base, but ensure ServerName is set
			config = e.upstreamTLSClientConfig.Clone()
			if config.ServerName == "" {
				config.ServerName = e.upstreamURL.Hostname()
			}
		}

		// Add HTTP/2 support via ALPN if requested
		if e.upstreamProtocol == "http2" {
			config.NextProtos = append(config.NextProtos, "h2", "http/1.1")
		}

		return tls.Client(conn, config), nil
	}

	return conn, nil
}

// join copies data bidirectionally between the two connections
func (e *EndpointForwarder) join(left, right net.Conn) {
	wg := &sync.WaitGroup{}
	wg.Add(2)

	// Copy from left to right
	go func() {
		defer wg.Done()
		defer right.Close() //nolint:errcheck
		_, _ = io.Copy(right, left)
	}()

	// Copy from right to left
	go func() {
		defer wg.Done()
		defer left.Close() //nolint:errcheck
		_, _ = io.Copy(left, right)
	}()

	wg.Wait()
}

// Close() is equivalent to for CloseWithContext(context.Background())
func (e *EndpointForwarder) Close() error {
	return e.CloseWithContext(context.Background())
}

// CloseWithContext closes the endpoint with the provided context.
func (e *EndpointForwarder) CloseWithContext(ctx context.Context) error {
	// Close via the listener
	err := e.listener.CloseWithContext(ctx)

	return wrapError(err)
}

// UpstreamProtocol returns the protocol used to communicate with the upstream server.
// This differs from UpstreamURL().Scheme if http2 is used.
func (e *EndpointForwarder) UpstreamProtocol() string {
	return e.upstreamProtocol
}

// UpstreamURL returns the URL that the endpoint forwards its traffic to.
func (e *EndpointForwarder) UpstreamURL() url.URL {
	return e.upstreamURL
}

// UpstreamTLSClientConfig returns the TLS client configuration used for upstream connections.
func (e *EndpointForwarder) UpstreamTLSClientConfig() *tls.Config {
	return e.upstreamTLSClientConfig
}

// ProxyProtocol returns the PROXY protocol version used for the endpoint.
// Returns a ProxyProtoVersion or empty string if not enabled.
func (e *EndpointForwarder) ProxyProtocol() ProxyProtoVersion {
	return e.proxyProtocol
}

// usesTLS checks if the provided scheme uses TLS
func usesTLS(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "https", "tls":
		return true
	default:
		return false
	}
}
