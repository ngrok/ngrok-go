package ngrok

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"strconv"
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

// UpdateableEndpointForwarder is implemented by EndpointForwarder types that support
// live updates to the upstream URL and mutable metadata fields. Use a type assertion
// to access it:
//
//	u, ok := forwarder.(ngrok.UpdateableEndpointForwarder)
type UpdateableEndpointForwarder interface {
	EndpointForwarder

	// UpdateUpstream atomically updates the upstream URL that connections are forwarded to.
	// Connections already in progress are not affected. Only new connections use the new URL.
	UpdateUpstream(u url.URL)

	// Update applies a partial update to the endpoint's mutable metadata fields
	// and propagates the change to the ngrok backend over the active session.
	Update(ctx context.Context, name, description, metadata *string, poolingEnabled *bool, trafficPolicy *string) error
}

// endpointForwarder implements the EndpointForwarder interface.
type endpointForwarder struct {
	baseEndpoint
	listener                *endpointListener
	upstreamURL             atomic.Pointer[url.URL]
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
	var header []byte
	var err error
	switch e.proxyProtocol {
	case ProxyProtoV1:
		for len(header) < 107 {
			var b [1]byte
			_, err = io.ReadFull(proxyConn, b[:])
			if err != nil {
				break
			}
			header = append(header, b[0])
			if len(header) >= 2 && string(header[len(header)-2:]) == "\r\n" {
				break
			}
		}
		if err == nil && !validProxyV1Header(header) {
			err = fmt.Errorf("invalid PROXY v1 header")
		}
	case ProxyProtoV2:
		header = make([]byte, 16)
		_, err = io.ReadFull(proxyConn, header)
		if err == nil && (string(header[:12]) != "\r\n\r\n\x00\r\nQUIT\n" || header[12]>>4 != 2 || header[12]&0x0f > 1) {
			err = fmt.Errorf("invalid PROXY v2 header framing")
		}
		// LOCAL ignores family and protocol, but still consumes the declared payload.
		if err == nil && header[12]&0x0f == 1 {
			family, protocol := header[13]>>4, header[13]&0x0f
			length := int(binary.BigEndian.Uint16(header[14:16]))
			if family > 3 || protocol > 2 {
				err = fmt.Errorf("invalid PROXY v2 family or protocol")
			} else if family != 0 && protocol != 0 && length < [...]int{0, 12, 36, 216}[family] {
				err = fmt.Errorf("short PROXY v2 address block")
			}
		}
		if err == nil {
			payload := make([]byte, int(binary.BigEndian.Uint16(header[14:16])))
			_, err = io.ReadFull(proxyConn, payload)
			header = append(header, payload...)
		}
	}
	if err != nil {
		_ = conn.Close()
		e.emitConnectionEvent(newConnectionClosed(e, remoteAddr, time.Since(start), proxyConn.bytesRead.Load(), 0))
		return
	}

	if e.isHTTP() {
		e.httpServe(proxyConn, header)
		e.emitConnectionEvent(newConnectionClosed(e, remoteAddr, time.Since(start), proxyConn.bytesRead.Load(), proxyConn.bytesWritten.Load()))
	} else {
		backend, err := e.connectToBackend(ctx, header)
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

func validProxyV1Header(header []byte) bool {
	line := string(header)
	if !strings.HasSuffix(line, "\r\n") {
		return false
	}
	fields := strings.Split(strings.TrimSuffix(line, "\r\n"), " ")
	if len(fields) < 2 || fields[0] != "PROXY" {
		return false
	}
	// UNKNOWN's remaining fields are explicitly opaque in the specification.
	if fields[1] == "UNKNOWN" {
		return true
	}
	if len(fields) != 6 || (fields[1] != "TCP4" && fields[1] != "TCP6") {
		return false
	}
	for _, field := range fields[2:4] {
		addr, err := netip.ParseAddr(field)
		if err != nil || addr.Zone() != "" || (fields[1] == "TCP4" && !addr.Is4()) || (fields[1] == "TCP6" && !addr.Is6()) {
			return false
		}
	}
	for _, field := range fields[4:6] {
		if field == "" || (len(field) > 1 && field[0] == '0') {
			return false
		}
		for _, digit := range field {
			if digit < '0' || digit > '9' {
				return false
			}
		}
		if _, err := strconv.ParseUint(field, 10, 16); err != nil {
			return false
		}
	}
	return true
}

func (e *endpointForwarder) emitConnectionEvent(evt Event) {
	if a, ok := e.agent.(*agent); ok {
		a.emitEvent(evt)
	}
}

// isHTTP reports whether connections are proxied as HTTP. Both the endpoint and
// the upstream must speak HTTP: a tcp or tls endpoint carries raw bytes even
// when its upstream URL has an http scheme, and parsing those bytes as HTTP
// stalls the connection.
func (e *endpointForwarder) isHTTP() bool {
	return isHTTPScheme(e.endpointURL.Scheme) && isHTTPScheme(e.upstreamURL.Load().Scheme)
}

func isHTTPScheme(scheme string) bool {
	switch strings.ToLower(scheme) {
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
func (e *endpointForwarder) connectToBackend(ctx context.Context, header []byte) (net.Conn, error) {
	u := e.upstreamURL.Load()

	// Parse host and port from URL
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		// Default ports based on scheme
		switch {
		case usesTLS(u.Scheme):
			port = "443"
		case strings.ToLower(u.Scheme) == "http":
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

	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", address)
	e.emitConnectionEvent(newUpstreamDialComplete(e, time.Since(start), err))
	if err != nil {
		return nil, err
	}
	if err := writeProxyHeader(conn, header); err != nil {
		return nil, err
	}

	// For HTTPS/TLS upstreams, establish TLS
	if usesTLS(u.Scheme) {
		config := &tls.Config{
			ServerName: u.Hostname(),
		}

		// Use custom TLS client config if provided
		if e.upstreamTLSClientConfig != nil {
			// Use the provided config as a base, but ensure ServerName is set
			config = e.upstreamTLSClientConfig.Clone()
			if config.ServerName == "" {
				config.ServerName = u.Hostname()
			}
		}

		// Add HTTP/2 support via ALPN if requested
		if e.upstreamProtocol == "http2" {
			config.NextProtos = append(config.NextProtos, "h2", "http/1.1")
		}

		return &handshakeReportingConn{
			Conn: tls.Client(conn, config),
			report: func(d time.Duration, err error) {
				e.emitConnectionEvent(newUpstreamTLSHandshakeComplete(e, d, err))
			},
		}, nil
	}

	return conn, nil
}

// writeProxyHeader sends edge metadata on the raw socket, before any TLS bytes.
func writeProxyHeader(conn net.Conn, header []byte) error {
	if len(header) == 0 {
		return nil
	}
	n, err := conn.Write(header)
	if err == nil && n != len(header) {
		err = io.ErrShortWrite
	}
	if err != nil {
		_ = conn.Close()
	}
	return err
}

// handshakeReportingConn runs the TLS handshake on the first Read or Write, as
// tls.Client does, and passes the duration and the result to report. Keep the
// handshake lazy. Inside join, a client that leaves before it sends data makes
// join close the connection, and the close ends a handshake that the upstream
// stalls.
type handshakeReportingConn struct {
	*tls.Conn
	report func(time.Duration, error)
	once   sync.Once
	err    error
}

func (c *handshakeReportingConn) handshake() error {
	c.once.Do(func() {
		start := time.Now()
		c.err = c.Handshake()
		c.report(time.Since(start), c.err)
	})
	return c.err
}

func (c *handshakeReportingConn) Read(p []byte) (int, error) {
	if err := c.handshake(); err != nil {
		return 0, err
	}
	return c.Conn.Read(p)
}

func (c *handshakeReportingConn) Write(p []byte) (int, error) {
	if err := c.handshake(); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
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
	return *e.upstreamURL.Load()
}

func (e *endpointForwarder) UpdateUpstream(u url.URL) {
	e.upstreamURL.Store(&u)
}

func (e *endpointForwarder) Update(ctx context.Context, name, description, metadata *string, poolingEnabled *bool, trafficPolicy *string) error {
	if a, ok := e.agent.(*agent); ok {
		if err := a.patchTunnelState(ctx, e.tunnelID, name, description, metadata, poolingEnabled, trafficPolicy); err != nil {
			return err
		}
	}
	e.update(name, description, metadata, poolingEnabled, trafficPolicy)
	return nil
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
