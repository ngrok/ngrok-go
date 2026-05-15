package ngrok

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"

	"golang.ngrok.com/ngrok/v2/internal/privatedial"
)

// DefaultPrivateDialServer is the address used when no
// WithPrivateDialServer option is provided. It points at the local devenv
// gateway; the prod and stage hostnames will replace this default as
// rollouts complete.
const DefaultPrivateDialServer = "dial-endpoint.ngrok.io.lan:443"

// PrivateDialTransport selects which HTTP transport a PrivateDialer uses
// to reach the gateway. Only TransportH2 is implemented today;
// TransportAuto is an alias for TransportH2 until the HTTP/3 leg ships.
type PrivateDialTransport string

const (
	// TransportAuto picks the best available transport. Today that is
	// always HTTP/2; it will become happy-eyeballs HTTP/3 → HTTP/2 once
	// HTTP/3 lands.
	TransportAuto PrivateDialTransport = "auto"
	// TransportH2 forces HTTP/2 over TLS.
	TransportH2 PrivateDialTransport = "h2"
	// TransportH3 forces HTTP/3 over QUIC. Not implemented; selecting
	// this transport causes NewPrivateDialer to return
	// ErrTransportUnavailable.
	TransportH3 PrivateDialTransport = "h3"
)

// ErrTransportUnavailable is returned by NewPrivateDialer when a caller
// asks for a transport this build cannot speak (today: HTTP/3).
var ErrTransportUnavailable = errors.New("private-dial: transport unavailable in this build")

// PrivateDialServerError is the error type wrapped inside *net.OpError on a
// non-200 /dial response. Use errors.As to extract the structured fields.
// The standard net sentinels are still reachable via errors.Is —
// *net.DNSError{IsNotFound:true} for 404, syscall.ECONNREFUSED for
// 401/403/429/503 — because the OpError's chain unwraps through here to the
// matching sentinel.
type PrivateDialServerError = privatedial.ServerError

// PrivateDialer opens TCP connections to private endpoints in the caller's
// account via the ngrok private-dial gateway. Each DialContext call
// multiplexes a new stream over a shared, lazily-established session.
type PrivateDialer interface {
	// DialContext opens a stream to the target address. network is
	// informational and expected to be "tcp"; address must be host:port.
	// The first call opens the underlying gateway session.
	DialContext(ctx context.Context, network, address string) (net.Conn, error)

	// Close tears down the underlying session, if any. In-flight streams
	// will see read/write errors.
	Close() error
}

// PrivateDialOption configures a PrivateDialer.
type PrivateDialOption func(*privateDialOpts)

type privateDialOpts struct {
	authToken     string
	server        string
	transport     PrivateDialTransport
	clientVersion string
	metadata      map[string]string
	tlsConfig     *tls.Config
}

// WithPrivateDialAuthtoken sets the tunnel authtoken presented as the
// Authorization Bearer credential on every request to the gateway.
func WithPrivateDialAuthtoken(token string) PrivateDialOption {
	return func(o *privateDialOpts) { o.authToken = token }
}

// WithPrivateDialServer overrides the gateway address. The value must be
// host:port (e.g. "dial-endpoint.stage-ngrok.io:443").
func WithPrivateDialServer(addr string) PrivateDialOption {
	return func(o *privateDialOpts) { o.server = addr }
}

// WithPrivateDialTransport pins the transport. Defaults to TransportAuto.
// Asking for TransportH3 in this build returns ErrTransportUnavailable
// from NewPrivateDialer.
func WithPrivateDialTransport(t PrivateDialTransport) PrivateDialOption {
	return func(o *privateDialOpts) { o.transport = t }
}

// WithPrivateDialClientVersion populates SessionReq.client_version and the
// outgoing User-Agent header for server-side logging.
func WithPrivateDialClientVersion(v string) PrivateDialOption {
	return func(o *privateDialOpts) { o.clientVersion = v }
}

// WithPrivateDialMetadata attaches free-form key/value metadata to the
// SessionReq for server-side logging.
func WithPrivateDialMetadata(m map[string]string) PrivateDialOption {
	return func(o *privateDialOpts) { o.metadata = m }
}

// WithPrivateDialTLSConfig overrides the TLS configuration used to reach
// the gateway. The dialer always forces TLS 1.3 minimum and ALPN h2 on
// the resulting config; other fields (RootCAs, InsecureSkipVerify, etc.)
// are honoured as supplied. Useful for stage/dev environments that
// terminate TLS with a custom CA.
func WithPrivateDialTLSConfig(cfg *tls.Config) PrivateDialOption {
	return func(o *privateDialOpts) { o.tlsConfig = cfg }
}

// NewPrivateDialer returns a PrivateDialer with the supplied options. It
// does not open any network connection; the first DialContext call opens
// the gateway session.
func NewPrivateDialer(opts ...PrivateDialOption) (PrivateDialer, error) {
	o := &privateDialOpts{
		server:    DefaultPrivateDialServer,
		transport: TransportAuto,
	}
	for _, opt := range opts {
		opt(o)
	}
	if o.authToken == "" {
		return nil, errors.New("private-dial: authtoken is required")
	}
	switch o.transport {
	case TransportAuto, TransportH2:
		// ok — TransportAuto resolves to H2 until H3 ships.
	case TransportH3:
		return nil, fmt.Errorf("%w: %q", ErrTransportUnavailable, o.transport)
	default:
		return nil, fmt.Errorf("private-dial: unknown transport %q", o.transport)
	}
	return &privateDialer{
		opts: o,
		client: privatedial.NewClient(privatedial.ClientOpts{
			ServerAddr:    o.server,
			AuthToken:     o.authToken,
			TLSConfig:     o.tlsConfig,
			ClientVersion: o.clientVersion,
			Metadata:      o.metadata,
		}),
	}, nil
}

type privateDialer struct {
	opts   *privateDialOpts
	client *privatedial.Client

	mu      sync.Mutex
	session *privatedial.Session
	closed  bool
}

func (d *privateDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := splitHostPort(address)
	if err != nil {
		return nil, &net.OpError{Op: "dial", Net: network, Err: err}
	}

	sess, err := d.activeSession(ctx)
	if err != nil {
		return nil, err
	}

	conn, err := sess.Dial(ctx, host, port)
	if err == nil {
		return conn, nil
	}

	// If the session is dead or draining, drop it and retry once on a
	// fresh session. A still-healthy session whose dial happened to fail
	// (e.g. 404 / 503 from the gateway) should NOT be torn down — the
	// caller will see a *net.OpError with the right sentinel.
	if d.shouldInvalidate(sess) {
		d.invalidate(sess)
		fresh, ferr := d.activeSession(ctx)
		if ferr != nil {
			return nil, err // surface the original dial error, not the reopen error
		}
		return fresh.Dial(ctx, host, port)
	}
	return nil, err
}

// activeSession returns a live session, opening a new one if necessary.
func (d *privateDialer) activeSession(ctx context.Context) (*privatedial.Session, error) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil, errors.New("private-dial: dialer is closed")
	}
	if d.session != nil && !sessionDead(d.session) {
		s := d.session
		d.mu.Unlock()
		return s, nil
	}
	// Drop a dead session so we don't leak the http2 transport.
	if d.session != nil {
		_ = d.session.Close()
		d.session = nil
	}
	d.mu.Unlock()

	sess, err := d.client.OpenSession(ctx)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		_ = sess.Close()
		return nil, errors.New("private-dial: dialer is closed")
	}
	// Lost a race with another DialContext; prefer the existing session.
	if d.session != nil && !sessionDead(d.session) {
		_ = sess.Close()
		return d.session, nil
	}
	d.session = sess
	return sess, nil
}

// shouldInvalidate reports whether a session that just failed a dial should
// be discarded before the retry.
func (d *privateDialer) shouldInvalidate(s *privatedial.Session) bool {
	return sessionDead(s)
}

func (d *privateDialer) invalidate(s *privatedial.Session) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.session == s {
		_ = s.Close()
		d.session = nil
	}
}

func (d *privateDialer) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	if d.session != nil {
		err := d.session.Close()
		d.session = nil
		return err
	}
	return nil
}

// sessionDead reports whether the session has received a drain signal or a
// terminal control-stream error. Either makes it unsafe for new dials.
func sessionDead(s *privatedial.Session) bool {
	select {
	case <-s.DrainCh():
		return true
	default:
	}
	select {
	case <-s.ServerErrCh():
		return true
	default:
	}
	return false
}

// splitHostPort parses a "host:port" address. Unlike net.SplitHostPort it
// also returns the numeric port — the private-dial protocol carries port
// as an int64 in the DialReq.
func splitHostPort(address string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, fmt.Errorf("invalid address %q: %w", address, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port %q: %w", portStr, err)
	}
	if port < 0 || port > 0xFFFF {
		return "", 0, fmt.Errorf("port %d out of range", port)
	}
	return host, port, nil
}
