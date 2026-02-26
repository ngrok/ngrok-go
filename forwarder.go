package ngrok

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// EndpointForwarder is an Endpoint that forwards traffic to an upstream service.
type EndpointForwarder interface {
	Endpoint

	// UpstreamProtocol returns the protocol used to communicate with the upstream server.
	// This differs from UpstreamURL().Scheme if http2 is used.
	UpstreamProtocol() string

	// UpstreamURL returns the URL that the endpoint forwards its traffic to.
	UpstreamURL() url.URL

	// UpstreamTLSClientConfig returns the TLS client configuration used for upstream connections.
	UpstreamTLSClientConfig() *tls.Config

	// ProxyProtocol returns the PROXY protocol version used for the endpoint.
	// Returns a ProxyProtoVersion or empty string if not enabled.
	ProxyProtocol() ProxyProtoVersion
}

// endpointForwarder implements the EndpointForwarder interface.
type endpointForwarder struct {
	baseEndpoint
	listener                *endpointListener
	upstreamURL             url.URL
	upstreamTLSClientConfig *tls.Config
	upstreamProtocol        string
	proxyProtocol           ProxyProtoVersion
	upstreamDialer          Dialer
}

// Start begins forwarding connections from the listener to the upstream URL
func (e *endpointForwarder) start(ctx context.Context) {
	go e.forwardLoop(ctx)
}

// forwardLoop is the main loop that forwards connections
func (e *endpointForwarder) forwardLoop(ctx context.Context) {
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
func (e *endpointForwarder) handleConnection(ctx context.Context, conn net.Conn) {
	start := time.Now()
	remoteAddr := conn.RemoteAddr().String()

	e.emitConnectionEvent(newConnectionOpened(e, remoteAddr))

	proxyConn := &countingConn{Conn: conn}

	if e.isHTTP() {
		e.httpServe(proxyConn)
		e.emitConnectionEvent(newConnectionClosed(e, remoteAddr, time.Since(start), proxyConn.bytesRead.Load(), proxyConn.bytesWritten.Load()))
	} else {
		// When proxy protocol is configured and the upstream uses TLS, the ngrok
		// edge prepends a PROXY header to the plaintext stream it sends us. We
		// must peel that header off here and write it to the raw backend TCP
		// connection before initiating TLS; if we don't, the header bytes end up
		// encrypted inside the TLS session and the backend never sees them as the
		// pre-TLS preamble it expects.
		var proxyHeader []byte
		if e.proxyProtocol != "" && usesTLS(e.upstreamURL.Scheme) {
			var err error
			proxyHeader, err = readProxyProtocolHeader(conn)
			if err != nil {
				conn.Close() //nolint:errcheck
				e.emitConnectionEvent(newConnectionClosed(e, remoteAddr, time.Since(start), 0, 0))
				return
			}
		}
		backend, err := e.connectToBackend(ctx, proxyHeader)
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

func (e *endpointForwarder) emitConnectionEvent(evt Event) {
	if a, ok := e.agent.(*agent); ok {
		a.emitEvent(evt)
	}
}

func (e *endpointForwarder) isHTTP() bool {
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

// connectToBackend establishes a connection to the upstream URL. If proxyHeader
// is non-nil, those bytes are written to the raw TCP connection before TLS is
// initiated, satisfying backends that expect a PROXY protocol preamble prior to
// the TLS handshake.
func (e *endpointForwarder) connectToBackend(ctx context.Context, proxyHeader []byte) (net.Conn, error) {
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

	// Write the PROXY protocol header to the raw TCP connection before TLS so
	// the backend sees it as a plaintext preamble preceding the handshake.
	if len(proxyHeader) > 0 {
		if _, err := conn.Write(proxyHeader); err != nil {
			conn.Close() //nolint:errcheck
			return nil, fmt.Errorf("write proxy protocol header: %w", err)
		}
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
func (e *endpointForwarder) join(left, right net.Conn) {
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

func (e *endpointForwarder) Close() error {
	return e.CloseWithContext(context.Background())
}

func (e *endpointForwarder) CloseWithContext(ctx context.Context) error {
	// Close via the listener
	err := e.listener.CloseWithContext(ctx)

	return wrapError(err)
}

// UpstreamProtocol returns the protocol used to communicate with the upstream server.
func (e *endpointForwarder) UpstreamProtocol() string {
	return e.upstreamProtocol
}

// UpstreamURL returns the URL that the endpoint forwards its traffic to.
func (e *endpointForwarder) UpstreamURL() url.URL {
	return e.upstreamURL
}

// UpstreamTLSClientConfig returns the TLS client configuration used for upstream connections.
func (e *endpointForwarder) UpstreamTLSClientConfig() *tls.Config {
	return e.upstreamTLSClientConfig
}

// ProxyProtocol returns the PROXY protocol version used for the endpoint.
func (e *endpointForwarder) ProxyProtocol() ProxyProtoVersion {
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

// readProxyProtocolHeader reads a PROXY protocol v1 or v2 header from r,
// consuming exactly the bytes that make up the header. After this returns
// successfully, r is positioned at the first byte of the payload (post-header).
//
// v1 format: "PROXY <proto> <src> <dst> <srcport> <dstport>\r\n"
// v2 format: 12-byte signature + ver/cmd byte + family byte + 2-byte addr-len + addr-len bytes
func readProxyProtocolHeader(r io.Reader) ([]byte, error) {
	first := [1]byte{}
	if _, err := io.ReadFull(r, first[:]); err != nil {
		return nil, fmt.Errorf("proxy protocol header: %w", err)
	}
	switch first[0] {
	case 'P': // v1 starts with "PROXY"
		rest, err := readProxyV1Tail(r)
		if err != nil {
			return nil, err
		}
		return append(first[:], rest...), nil
	case 0x0D: // v2 signature begins with \r\n\r\n...
		rest, err := readProxyV2Tail(r)
		if err != nil {
			return nil, err
		}
		return append(first[:], rest...), nil
	default:
		return nil, fmt.Errorf("proxy protocol: unrecognized signature byte 0x%02x", first[0])
	}
}

// readProxyV1Tail reads the remainder of a PROXY v1 line after the leading 'P'.
// The spec caps the total line length at 108 bytes.
func readProxyV1Tail(r io.Reader) ([]byte, error) {
	const maxTail = 107 // 108 total minus the 'P' already consumed
	buf := make([]byte, 0, 64)
	b := [1]byte{}
	for len(buf) < maxTail {
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, fmt.Errorf("proxy protocol v1: %w", err)
		}
		buf = append(buf, b[0])
		if b[0] == '\n' && len(buf) >= 2 && buf[len(buf)-2] == '\r' {
			return buf, nil
		}
	}
	return nil, fmt.Errorf("proxy protocol v1: header exceeds maximum length")
}

// readProxyV2Tail reads the remainder of a PROXY v2 header after the leading
// 0x0D byte. The fixed header is 16 bytes total; the address block length is
// the big-endian uint16 at bytes 14-15.
func readProxyV2Tail(r io.Reader) ([]byte, error) {
	// Need 15 more bytes to complete the 16-byte fixed header.
	fixed := make([]byte, 15)
	if _, err := io.ReadFull(r, fixed); err != nil {
		return nil, fmt.Errorf("proxy protocol v2 fixed header: %w", err)
	}
	// Bytes 14-15 of the full header are at indices 13-14 of `fixed`
	// (we already consumed byte 0).
	addrLen := binary.BigEndian.Uint16(fixed[13:15])
	addr := make([]byte, addrLen)
	if _, err := io.ReadFull(r, addr); err != nil {
		return nil, fmt.Errorf("proxy protocol v2 address data: %w", err)
	}
	return append(fixed, addr...), nil
}
