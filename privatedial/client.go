// Package privatedial implements a client to allow dialing private ngrok
// endpoints. It authenticates with an API Key as its authtoken and then multiplexes
// per-target net.Conn streams over a single HTTP/2 or HTTP/3
// connection. A Session transparently reconnects on server drain or abrupt
// control-stream failure; new Dial calls follow the freshest underlying
// transport while in-flight streams ride out the server-advertised drain grace
// period on the old one.
package privatedial

import (
	"bufio"
	"context"
	cryptorand "crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand/v2"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"golang.org/x/net/http2"
	"google.golang.org/protobuf/encoding/protodelim"

	pbpd "golang.ngrok.com/ngrok/privatedial/pb_private_dial"
)

// Protocol selects the transport used to reach the private-dial server.
type Protocol int

const (
	// ProtocolAuto races HTTP/3 (QUIC) against HTTP/2 in a Happy-Eyeballs
	// fashion, preferring HTTP/3. This is the default.
	ProtocolAuto Protocol = iota
	// ProtocolQUIC forces HTTP/3 only (e.g. when the caller knows TCP is
	// degraded).
	ProtocolQUIC
	// ProtocolH2 forces HTTP/2 only (e.g. when the caller knows UDP is
	// blocked).
	ProtocolH2
)

// String returns a human-readable name for the protocol, using the wire
// names ("HTTP/3", "HTTP/2") for the concrete transports.
func (p Protocol) String() string {
	switch p {
	case ProtocolAuto:
		return "auto"
	case ProtocolQUIC:
		return "HTTP/3"
	case ProtocolH2:
		return "HTTP/2"
	default:
		return fmt.Sprintf("Protocol(%d)", int(p))
	}
}

const (
	// dialTimeout bounds a single connection attempt during the race or reconnect.
	dialTimeout = 3 * time.Second
	// quicHeadStart is how long the QUIC attempt runs alone before the
	// HTTP/2 attempt is staggered in. If QUIC completes within this window
	// no second connection is ever made.
	quicHeadStart = 250 * time.Millisecond
)

// stickyProtocol records the first protocol the race settled on in this
// process. Once set, OpenSession reuses it for every subsequent session
// (including reconnects) and skips the happy-eyeballs race entirely.
var stickyProtocol atomic.Pointer[Protocol]

// roundTripCloser is the subset of transport behavior the session relies on.
// Holding the transport behind this interface lets a single Session
// implementation drive either protocol.
type roundTripCloser interface {
	RoundTrip(*http.Request) (*http.Response, error)
	CloseIdleConnections()
}

type requestReserver interface {
	ReserveNewRequest() bool
}

type closeTransport interface {
	Close() error
}

type sessionTimer interface {
	C() <-chan time.Time
	Stop() bool
}

type sessionClock interface {
	NewTimer(time.Duration) sessionTimer
}

type realClock struct{}

func (realClock) NewTimer(d time.Duration) sessionTimer {
	return realTimer{timer: time.NewTimer(d)}
}

type realTimer struct {
	timer *time.Timer
}

func (t realTimer) C() <-chan time.Time { return t.timer.C }
func (t realTimer) Stop() bool          { return t.timer.Stop() }

type h2ClientConnTransport struct {
	cc *http2.ClientConn
}

func (t *h2ClientConnTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.cc.RoundTrip(req)
}

func (t *h2ClientConnTransport) ReserveNewRequest() bool {
	return t.cc.ReserveNewRequest()
}

func (t *h2ClientConnTransport) CloseIdleConnections() {
	_ = t.Close()
}

func (t *h2ClientConnTransport) Close() error {
	return t.cc.Close()
}

// protodelimUnmarshaler caps inbound server frames. The server only sends
// SessionAck and small ControlFrames (Ping/Pong/PleaseDrain/SessionError),
// so 16k is far more than legitimate traffic needs.
var protodelimUnmarshaler = &protodelim.UnmarshalOptions{
	MaxSize: 16 * 1024,
}

// readCloseByteReader adapts an io.ReadCloser into the Read+ByteReader
// surface protodelim.UnmarshalFrom needs, while keeping Close routed to
// the original body.
type readCloseByteReader struct {
	*bufio.Reader
	closer io.Closer
}

func newReadCloseByteReader(rc io.ReadCloser) *readCloseByteReader {
	return &readCloseByteReader{
		Reader: bufio.NewReader(rc),
		closer: rc,
	}
}

func (r *readCloseByteReader) Close() error { return r.closer.Close() }

// ClientOpts configures a Client.
type ClientOpts struct {
	// ServerAddr is the "host:port" endpoint that serves the private-dial
	// protocol (e.g. "h2.connect-endpoint.ngrok.com:443"). The net.Dial to
	// this address must reach a mux PrivateDialIngresses listener. It is the
	// default for both QUICServerAddr and H2ServerAddr.
	ServerAddr string

	// QUICServerAddr is the "host:port" used for the HTTP/3 (QUIC) attempt
	// (e.g. "quic.connect-endpoint.ngrok.com:443"). Defaults to ServerAddr.
	QUICServerAddr string

	// H2ServerAddr is the "host:port" used for the HTTP/2 attempt
	// (e.g. "h2.connect-endpoint.ngrok.com:443"). Defaults to ServerAddr.
	H2ServerAddr string

	// ForceProtocol, when not ProtocolAuto, skips the Happy-Eyeballs race
	// and uses only the named transport. Useful when the caller knows that
	// UDP (force ProtocolH2) or TCP (force ProtocolQUIC) is unavailable.
	ForceProtocol Protocol

	// ServerName overrides the SNI name used when negotiating TLS. When
	// empty, the SNI defaults to the host portion of the per-protocol
	// server address.
	ServerName string

	// AuthToken is the auth token to use. During development, this is an ngrok
	// API Key, it'll be a proper token eventually.
	AuthToken string

	// TLSConfig overrides the default TLS config. MinVersion and NextProtos
	// are forced to TLS 1.3 / h2 respectively regardless.
	TLSConfig *tls.Config

	// ClientVersion is metadata about this client.
	ClientVersion string

	// Metadata is arbitrary metadata about this client. This will be made
	// available (TODO) in the cel environment of endpoints receiving requests
	// from this client.
	Metadata map[string]string
}

// Client is a reusable factory for private-dial sessions.
type Client struct {
	opts ClientOpts
}

// NewClient returns a Client. It does not open any connection.
func NewClient(opts ClientOpts) *Client {
	if opts.QUICServerAddr == "" {
		opts.QUICServerAddr = opts.ServerAddr
	}
	if opts.H2ServerAddr == "" {
		opts.H2ServerAddr = opts.ServerAddr
	}
	return &Client{opts: opts}
}

// serverNameFor returns the SNI to use for a given server address: the
// explicit ServerName override when set, otherwise the host portion of addr.
func (c *Client) serverNameFor(addr string) string {
	if c.opts.ServerName != "" {
		return c.opts.ServerName
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// OpenSession establishes a single connection to the server, opens the
// control stream (/session), authenticates, and returns a Session. The
// caller must Close the Session to release the connection. The Session
// reconnects on server PleaseDrain or abrupt control-stream failure.
//
// Transport selection follows the spec's Happy-Eyeballs-like algorithm: by
// default it races HTTP/3 (QUIC) against HTTP/2, preferring QUIC, and
// remembers the winning protocol process-wide so later sessions skip the
// race. ClientOpts.ForceProtocol overrides this.
//
// The caller's ctx scopes the initial handshake only. Once OpenSession returns,
// canceling ctx has no effect on the Session.
func (c *Client) OpenSession(ctx context.Context) (*Session, error) {
	proto := c.opts.ForceProtocol
	if proto == ProtocolAuto {
		stickyProto := stickyProtocol.Load()
		if stickyProto != nil {
			proto = *stickyProto
		}
	}
	switch proto {
	case ProtocolQUIC, ProtocolH2:
		// Forced, or a previous race already settled on a protocol.
		return c.openProtocol(ctx, proto)
	default:
		return c.race(ctx)
	}
}

// race implements the spec's staggered Happy-Eyeballs algorithm: start the
// QUIC attempt, give it a head start, then stagger in the HTTP/2 attempt;
// the first to produce a usable Session wins (QUIC preferred), the loser is
// cancelled and any session it raced through is closed.
func (c *Client) race(ctx context.Context) (*Session, error) {
	type result struct {
		proto Protocol
		sess  *Session
		err   error
	}

	launch := func(p Protocol) (chan result, context.CancelFunc) {
		attemptCtx, cancel := context.WithTimeout(ctx, dialTimeout)
		ch := make(chan result, 1)
		go func() {
			sess, err := c.openProtocol(attemptCtx, p)
			ch <- result{proto: p, sess: sess, err: err}
		}()
		return ch, cancel
	}

	// closeLoser cancels a still-running attempt and closes the session it
	// produced if it happened to win the race after we'd already committed.
	closeLoser := func(ch chan result, cancel context.CancelFunc) {
		cancel()
		go func() {
			if r := <-ch; r.sess != nil {
				_ = r.sess.Close()
			}
		}()
	}

	quicCh, quicCancel := launch(ProtocolQUIC)

	// Phase 1: QUIC runs alone for quicHeadStart. If it lands inside the
	// window we use it without ever making a second connection.
	var quicErr error
	quicDone := false
	timer := time.NewTimer(quicHeadStart)
	select {
	case r := <-quicCh:
		timer.Stop()
		quicDone = true
		if r.err == nil {
			quicCancel()
			stickyProtocol.CompareAndSwap(nil, new(ProtocolQUIC))
			return r.sess, nil
		}
		quicErr = r.err
	case <-timer.C:
		// QUIC still in flight; stagger in HTTP/2 below.
	}

	// Phase 2: HTTP/2 joins the race. First success wins, QUIC preferred.
	h2Ch, h2Cancel := launch(ProtocolH2)
	var h2Err error
	h2Done := false

	// Disable the QUIC select arm if Phase 1 already drained it.
	pendingQuic := quicCh
	if quicDone {
		pendingQuic = nil
	}

	for !quicDone || !h2Done {
		select {
		case r := <-pendingQuic:
			quicDone = true
			pendingQuic = nil
			if r.err == nil {
				quicCancel()
				if h2Done {
					h2Cancel()
				} else {
					closeLoser(h2Ch, h2Cancel)
				}
				stickyProtocol.CompareAndSwap(nil, new(ProtocolQUIC))
				return r.sess, nil
			}
			quicErr = r.err
		case r := <-h2Ch:
			h2Done = true
			if r.err == nil {
				h2Cancel()
				if quicDone {
					quicCancel()
				} else {
					closeLoser(quicCh, quicCancel)
				}
				stickyProtocol.CompareAndSwap(nil, new(ProtocolH2))
				return r.sess, nil
			}
			h2Err = r.err
		}
	}

	quicCancel()
	h2Cancel()
	return nil, fmt.Errorf("private-dial: both transports failed: quic=%v, h2=%v", quicErr, h2Err)
}

// openProtocol builds the transport for p and runs the /session handshake
// over it.
func (c *Client) openProtocol(ctx context.Context, p Protocol) (*Session, error) {
	first, err := c.openConn(ctx, p)
	if err != nil {
		return nil, err
	}

	sessCtx, cancel := context.WithCancel(context.Background())
	s := &Session{
		ctx:         sessCtx,
		cancel:      cancel,
		proto:       p,
		ready:       make(chan struct{}),
		current:     first,
		drainCh:     make(chan struct{}),
		serverErrCh: make(chan error, 1),
		openFn: func(ctx context.Context) (*sessionConn, error) {
			return c.openConn(ctx, p)
		},
	}
	go s.supervise()
	return s, nil
}

func (c *Client) openConn(ctx context.Context, p Protocol) (*sessionConn, error) {
	var (
		transport  roundTripCloser
		serverAddr string
		remoteAddr string
		rec        *connAddrRecorder
		err        error
	)
	switch p {
	case ProtocolQUIC:
		transport, rec = c.newH3Transport()
		serverAddr = c.opts.QUICServerAddr
	case ProtocolH2:
		transport, remoteAddr, err = c.newH2Transport(ctx)
		if err != nil {
			return nil, err
		}
		serverAddr = c.opts.H2ServerAddr
	default:
		return nil, fmt.Errorf("private-dial: unsupported protocol %d", p)
	}

	h := &sessionConn{
		proto:      p,
		transport:  transport,
		remoteAddr: remoteAddr,
		stopCh:     make(chan struct{}),
	}

	controlURL := &url.URL{Scheme: "https", Host: serverAddr, Path: "/session"}
	dialURL := &url.URL{Scheme: "https", Host: serverAddr, Path: "/dial"}

	// io.Pipe for the request body — first frame is SessionReq, then
	// the body is reused for client-→server ControlFrames (Ping/Pong).
	bodyReader, bodyWriter := io.Pipe()
	h.controlReqBody = bodyWriter
	reqCtx, reqCancel := context.WithCancel(context.Background())
	h.controlCancel = reqCancel
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, controlURL.String(), bodyReader)
	if err != nil {
		_ = h.close()
		return nil, fmt.Errorf("build /session request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.opts.AuthToken)
	req.Header.Set("Content-Type", "application/x-protobuf")
	if c.opts.ClientVersion != "" {
		req.Header.Set("User-Agent", c.opts.ClientVersion)
	}

	// Write SessionReq concurrently with RoundTrip — the server reads
	// it before sending headers, so RoundTrip won't return until this
	// hits the wire.
	go func() {
		_, err := protodelim.MarshalTo(bodyWriter, &pbpd.SessionReq{
			ClientVersion: c.opts.ClientVersion,
			Metadata:      c.opts.Metadata,
		})
		if err != nil {
			_ = bodyWriter.CloseWithError(err)
		}
	}()

	type roundTripResult struct {
		resp *http.Response
		err  error
	}
	roundTripCh := make(chan roundTripResult, 1)
	go func() {
		resp, err := transport.RoundTrip(req)
		roundTripCh <- roundTripResult{resp: resp, err: err}
	}()

	var resp *http.Response
	select {
	case result := <-roundTripCh:
		resp, err = result.resp, result.err
	case <-ctx.Done():
		_ = h.close()
		return nil, fmt.Errorf("private-dial /session: %w", ctx.Err())
	}
	if err != nil {
		_ = h.close()
		return nil, fmt.Errorf("private-dial /session: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		_ = h.close()
		err := fmt.Errorf("private-dial /session status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, &authFatalError{status: resp.StatusCode, err: err}
		}
		return nil, err
	}
	respBody := newReadCloseByteReader(resp.Body)
	resp.Body = respBody

	ack := new(pbpd.SessionAck)
	if err := protodelimUnmarshaler.UnmarshalFrom(respBody, ack); err != nil {
		_ = h.close()
		return nil, fmt.Errorf("read SessionAck: %w", err)
	}

	if rec != nil {
		h.remoteAddr = rec.get()
	}
	h.serverID = ack.GetServerId()
	h.pingInterval = ack.GetPingInterval().AsDuration()
	h.dialURL = dialURL
	h.authToken = c.opts.AuthToken
	h.controlRespBody = respBody
	h.sendCh = make(chan *pbpd.ControlFrame)
	h.sendDone = make(chan struct{})
	h.pings = map[uint64]time.Time{}
	h.drainCh = make(chan struct{})
	h.serverErrCh = make(chan error, 1)

	go h.controlSender()
	go h.readControl()
	if h.pingInterval > 0 {
		go h.pingLoop()
	}
	return h, nil
}

// tlsConfigFor clones the configured TLS settings and pins them for the
// given server address and ALPN protocol.
func (c *Client) tlsConfigFor(serverAddr string, nextProtos ...string) *tls.Config {
	tlsCfg := &tls.Config{}
	if c.opts.TLSConfig != nil {
		tlsCfg = c.opts.TLSConfig.Clone()
	}
	tlsCfg.ServerName = c.serverNameFor(serverAddr)
	tlsCfg.NextProtos = nextProtos
	if tlsCfg.MinVersion == 0 {
		tlsCfg.MinVersion = tls.VersionTLS13
	}
	return tlsCfg
}

// connAddrRecorder captures the remote address of a transport connection as
// it is dialed. The dial happens during RoundTrip, but the mutex keeps tests
// race-detector-clean when the address is read after the handshake.
type connAddrRecorder struct {
	mu   sync.Mutex
	addr string
}

func (r *connAddrRecorder) set(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addr = addr
}

func (r *connAddrRecorder) get() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.addr
}

// newH2Transport builds an owned HTTP/2 ClientConn for one private-dial
// control stream and its /dial streams. This avoids http2.Transport pooling
// across logical sessions.
func (c *Client) newH2Transport(ctx context.Context) (roundTripCloser, string, error) {
	tlsConn, err := (&tls.Dialer{Config: c.tlsConfigFor(c.opts.H2ServerAddr, "h2")}).
		DialContext(ctx, "tcp", c.opts.H2ServerAddr)
	if err != nil {
		return nil, "", fmt.Errorf("private-dial: tls dial: %w", err)
	}
	remoteAddr := tlsConn.RemoteAddr().String()
	transport := &http2.Transport{
		ReadIdleTimeout: 30 * time.Second,
		PingTimeout:     15 * time.Second,
	}
	cc, err := transport.NewClientConn(tlsConn)
	if err != nil {
		_ = tlsConn.Close()
		return nil, "", fmt.Errorf("private-dial: h2 client conn: %w", err)
	}
	return &h2ClientConnTransport{cc: cc}, remoteAddr, nil
}

// newH3Transport builds the HTTP/3 (QUIC) transport and records the server
// address the QUIC connection lands on. KeepAlivePeriod is the QUIC analogue
// of the h2 keepalive PINGs: it keeps the single multiplexing connection alive
// and surfaces a dead peer to stuck stream writers.
func (c *Client) newH3Transport() (roundTripCloser, *connAddrRecorder) {
	rec := &connAddrRecorder{}
	t := &http3.Transport{
		TLSClientConfig: c.tlsConfigFor(c.opts.QUICServerAddr, "h3"),
		QUICConfig: &quic.Config{
			KeepAlivePeriod: 30 * time.Second,
		},
	}
	t.Dial = func(ctx context.Context, addr string, tlsCfg *tls.Config, cfg *quic.Config) (*quic.Conn, error) {
		udpAddr, err := resolveUDPAddr(ctx, addr)
		if err != nil {
			return nil, err
		}
		pc, err := net.ListenUDP("udp", nil)
		if err != nil {
			return nil, err
		}
		conn, err := quic.DialEarly(ctx, pc, udpAddr, tlsCfg, cfg)
		if err != nil {
			_ = pc.Close()
			return nil, err
		}
		rec.set(conn.RemoteAddr().String())
		context.AfterFunc(conn.Context(), func() { _ = pc.Close() })
		return conn, nil
	}
	return t, rec
}

// resolveUDPAddr resolves a "host:port" to a single *net.UDPAddr, honoring ctx
// for cancellation. It mirrors the resolution the default http3 dial performs.
func resolveUDPAddr(ctx context.Context, addr string) (*net.UDPAddr, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := net.LookupPort("udp", portStr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no address for %s", host)
	}
	return net.UDPAddrFromAddrPort(netip.AddrPortFrom(ips[0].Unmap(), uint16(port))), nil
}

// authFatalError marks /session rejections that should not be retried.
type authFatalError struct {
	status int
	err    error
}

func (e *authFatalError) Error() string { return e.err.Error() }
func (e *authFatalError) Unwrap() error { return e.err }

// Session represents an authenticated private-dial session. It reconnects on
// server drain and control-stream failures while routing new Dial calls to the
// freshest underlying transport.
type Session struct {
	ctx    context.Context
	cancel context.CancelFunc

	// proto is the transport this session settled on (ProtocolQUIC or
	// ProtocolH2). Set once at open time.
	proto Protocol

	// openFn opens replacement per-transport connections.
	openFn func(context.Context) (*sessionConn, error)

	clock sessionClock

	drainGroup sync.WaitGroup

	mu       sync.Mutex
	current  *sessionConn
	fatalErr error
	// ready is closed every time current is replaced or fatalErr is set.
	ready chan struct{}

	dialWait time.Duration

	drainOnce   sync.Once
	drainCh     chan struct{}
	serverErrCh chan error

	closeOnce sync.Once
}

const defaultDialWait = 5 * time.Second

// ServerID returns the opaque identifier the server emitted in the most recent
// SessionAck, or the empty string if no conn is currently established.
func (s *Session) ServerID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return ""
	}
	return s.current.serverID
}

// Protocol returns the transport this session settled on — ProtocolQUIC
// (HTTP/3) or ProtocolH2 (HTTP/2).
func (s *Session) Protocol() Protocol { return s.proto }

// RemoteAddr returns the server address (host:port) the current underlying
// transport connection landed on. It returns an empty string if no connection
// is currently established.
func (s *Session) RemoteAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return ""
	}
	return s.current.remoteAddr
}

// PingInterval is the cadence at which the server expects to send and receive
// Ping frames on the current control stream. Zero if no conn is established.
func (s *Session) PingInterval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return 0
	}
	return s.current.pingInterval
}

// LastRTT returns the most recent round-trip time measured by the client-side
// ping loop on the current conn. Zero before the first pong arrives, and reset
// to zero across reconnects.
func (s *Session) LastRTT() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return 0
	}
	return s.current.LastRTT()
}

// DrainCh is closed when any underlying connection receives PleaseDrain. The
// Session will reconnect automatically; this method remains for compatibility
// with callers that want to observe drain events.
func (s *Session) DrainCh() <-chan struct{} { return s.drainCh }

// ServerErrCh delivers fatal logical-session errors, such as non-retryable auth
// failures during reconnect. Transient control-stream failures are consumed by
// the reconnect supervisor.
func (s *Session) ServerErrCh() <-chan error { return s.serverErrCh }

// DialContext opens a new stream targeting addr (of the form 'host:port').
// The port _must_ be numeric, and the host must refer to a private endpoint
// within this Session's associated account. For example, if this account has
// an endpoint created with 'ngrok http --url "http://foo.internal" 8080', the
// expected invocation to reach that endpoint would be:
//
//	conn, err := dialer.DialContext(ctx, "tcp", "foo.internal:80")
//
// The returned net.Conn is a bidirectional byte stream.
//
// Server-side dial failures are translated to standard net errors so
// callers can use errors.Is/As against the usual sentinels:
//   - "endpoint not found" returns a *net.OpError wrapping
//     *net.DNSError (with IsNotFound: true) — analogous to a DNS
//     lookup miss.
//   - "no endpoint available" / "session draining" returns a
//     *net.OpError wrapping syscall.ECONNREFUSED — analogous to a
//     refused TCP connect.
//   - other non-200 responses return a generic *net.OpError with the
//     server's error body as the wrapped Err.
func (s *Session) DialContext(ctx context.Context, network string, addr string) (net.Conn, error) {
	if network != "tcp" {
		// we only support tcp endpoints
		return nil, net.UnknownNetworkError(network)
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, &net.OpError{Op: "dial", Net: network, Err: fmt.Errorf("invalid addr: %w", err)}
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, &net.OpError{Op: "dial", Net: network, Err: fmt.Errorf("invalid addr port: %w", err)}
	}

	// we validated 'addr' is well formed, so we can just return it up to callers
	// and save having to format in the port each time.
	return s.dial(ctx, dialAddr{
		addr: addr,
		host: host,
		port: int(port),
	})
}

func (s *Session) dial(ctx context.Context, addr dialAddr) (net.Conn, error) {
	timer := s.sessionClock().NewTimer(s.dialWaitTimeout())
	defer timer.Stop()

	dialErrFor := func(err error) error {
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return &net.OpError{
				Op:   "dial",
				Net:  addr.Network(),
				Addr: addr,
				Err:  &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED},
			}
		}
		return &net.OpError{Op: "dial", Net: addr.Network(), Addr: addr, Err: err}
	}

	for {
		select {
		case <-timer.C():
			return nil, dialErrFor(context.DeadlineExceeded)
		default:
		}
		if err := ctx.Err(); err != nil {
			return nil, dialErrFor(err)
		}
		cur, err := s.waitForCurrent(ctx, timer.C())
		if err != nil {
			return nil, dialErrFor(err)
		}
		conn, dialErr := cur.dial(ctx, addr)
		if dialErr == nil {
			return conn, nil
		}
		if errors.Is(dialErr, context.Canceled) || errors.Is(dialErr, context.DeadlineExceeded) {
			return nil, dialErr
		}
		var stale *staleConnError
		if !errors.As(dialErr, &stale) {
			return nil, dialErr
		}
	}
}

func (s *Session) waitForCurrent(ctx context.Context, budget <-chan time.Time) (*sessionConn, error) {
	for {
		if err := s.ctx.Err(); err != nil {
			return nil, errors.New("private-dial: session closed")
		}
		cur, ready, fatal := s.snapshot()
		if fatal != nil {
			return nil, fatal
		}
		if cur != nil && cur.acceptsStreams() {
			return cur, nil
		}
		select {
		case <-ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-budget:
			return nil, context.DeadlineExceeded
		case <-s.ctx.Done():
			return nil, errors.New("private-dial: session closed")
		}
	}
}

type staleConnError struct {
	addr    dialAddr
	wrapped error
}

func (e *staleConnError) Error() string {
	if e.wrapped != nil {
		return fmt.Sprintf("private-dial: stale conn: %s", e.wrapped)
	}
	return "private-dial: stale conn"
}

func (e *staleConnError) Unwrap() error { return e.wrapped }

func newStaleConnError(addr dialAddr) *staleConnError {
	return &staleConnError{addr: addr}
}

func (s *Session) dialWaitTimeout() time.Duration {
	if s.dialWait > 0 {
		return s.dialWait
	}
	return defaultDialWait
}

func (s *Session) snapshot() (*sessionConn, <-chan struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current, s.ready, s.fatalErr
}

func (s *Session) supervise() {
	for {
		s.mu.Lock()
		cur := s.current
		s.mu.Unlock()
		if cur == nil {
			next, err := s.reconnect()
			if err != nil {
				return
			}
			s.swapCurrent(next)
			continue
		}
		select {
		case <-s.ctx.Done():
			return
		case <-cur.drainCh:
			s.parkDraining(cur, cur.drainGrace)
		case <-cur.serverErrCh:
			select {
			case <-cur.drainCh:
				s.parkDraining(cur, cur.drainGrace)
			default:
				s.removeCurrent(cur)
			}
		}
	}
}

func (s *Session) reconnect() (*sessionConn, error) {
	boff := newReconnectBackoff(reconnectBackoffMinDelay, reconnectBackoffMaxDelay, s.sessionClock())
	for {
		if err := s.ctx.Err(); err != nil {
			return nil, err
		}
		attemptCtx, cancel := context.WithTimeout(s.ctx, dialTimeout)
		h, err := s.openFn(attemptCtx)
		cancel()
		if err == nil {
			if cerr := s.ctx.Err(); cerr != nil {
				_ = h.close()
				return nil, cerr
			}
			return h, nil
		}
		var fatal *authFatalError
		if errors.As(err, &fatal) {
			s.setFatal(err)
			return nil, err
		}
		if err := boff.Wait(s.ctx); err != nil {
			return nil, err
		}
	}
}

const reconnectBackoffMinDelay = 8 * time.Millisecond
const reconnectBackoffMaxDelay = 32768 * time.Millisecond

type reconnectBackoff struct {
	min   time.Duration
	max   time.Duration
	next  time.Duration
	clock sessionClock
}

func newReconnectBackoff(minDelay, maxDelay time.Duration, clock sessionClock) *reconnectBackoff {
	return &reconnectBackoff{min: minDelay, max: maxDelay, next: minDelay, clock: clock}
}

func (b *reconnectBackoff) Wait(ctx context.Context) error {
	delay := b.next
	if b.next < b.max {
		jitter := 0.25 * ((2 * mathrand.Float64()) - 1)
		next := time.Duration(float64(b.next) * (1.5 + jitter))
		if next > b.max {
			next = b.max
		}
		b.next = next
	}

	timer := b.clock.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Session) sessionClock() sessionClock {
	if s.clock != nil {
		return s.clock
	}
	return realClock{}
}

func (s *Session) swapCurrent(h *sessionConn) {
	s.mu.Lock()
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		_ = h.close()
		return
	}
	s.current = h
	s.signalReadyLocked()
	s.mu.Unlock()
}

func (s *Session) parkDraining(old *sessionConn, grace time.Duration) {
	s.drainOnce.Do(func() { close(s.drainCh) })

	closeNow := false
	s.mu.Lock()
	if s.ctx.Err() != nil {
		closeNow = true
	} else {
		if s.current == old {
			s.current = nil
		}
		s.signalReadyLocked()

		if grace <= 0 {
			closeNow = true
		} else {
			clk := s.sessionClock()
			s.drainGroup.Add(1)
			go func() {
				defer s.drainGroup.Done()
				timer := clk.NewTimer(grace)
				defer timer.Stop()
				select {
				case <-timer.C():
				case <-s.ctx.Done():
				}
				_ = old.close()
			}()
		}
	}
	s.mu.Unlock()
	if closeNow {
		_ = old.close()
	}
}

func (s *Session) removeCurrent(cur *sessionConn) {
	s.mu.Lock()
	if s.current == cur {
		s.current = nil
		s.signalReadyLocked()
	}
	s.mu.Unlock()
	_ = cur.close()
}

func (s *Session) setFatal(err error) {
	s.mu.Lock()
	if s.fatalErr == nil {
		s.fatalErr = err
		select {
		case s.serverErrCh <- err:
		default:
		}
	}
	s.signalReadyLocked()
	s.mu.Unlock()
}

func (s *Session) signalReadyLocked() {
	close(s.ready)
	s.ready = make(chan struct{})
}

// Close tears down the supervisor, the active conn, and any conns still in
// their drain grace window. In-flight Dial streams will see read/write errors.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		s.mu.Lock()
		cur := s.current
		s.current = nil
		s.signalReadyLocked()
		s.mu.Unlock()
		if cur != nil {
			_ = cur.close()
		}
		s.drainGroup.Wait()
	})
	return nil
}

type sessionConn struct {
	serverID     string
	pingInterval time.Duration
	proto        Protocol
	remoteAddr   string

	transport roundTripCloser

	lifeMu      sync.Mutex
	lifeClosed  bool
	lifeDrained bool

	dialURL   *url.URL
	authToken string

	controlRespBody *readCloseByteReader
	controlReqBody  *io.PipeWriter
	controlCancel   context.CancelFunc

	sendCh   chan *pbpd.ControlFrame
	sendDone chan struct{}

	pingsMu sync.Mutex
	pings   map[uint64]time.Time

	drainOnce  sync.Once
	drainCh    chan struct{}
	drainGrace time.Duration

	serverErrCh chan error

	stopCh chan struct{}

	rttMu   sync.Mutex
	lastRTT time.Duration

	closeOnce sync.Once
}

func (h *sessionConn) LastRTT() time.Duration {
	h.rttMu.Lock()
	defer h.rttMu.Unlock()
	return h.lastRTT
}

func (h *sessionConn) dial(ctx context.Context, addr dialAddr) (net.Conn, error) {
	reqReader, reqWriter := io.Pipe()
	reqCtx, reqCancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, h.dialURL.String(), reqReader)
	if err != nil {
		_ = reqWriter.Close()
		reqCancel()
		return nil, &net.OpError{Op: "dial", Net: addr.Network(), Addr: addr, Err: fmt.Errorf("build /dial request: %w", err)}
	}
	req.Header.Set("Authorization", "Bearer "+h.authToken)
	req.Header.Set("Content-Type", "application/octet-stream")

	dreq := &pbpd.DialReq{
		Host: addr.host,
		Port: int64(addr.port),
	}

	if !h.acceptsStreams() {
		_ = reqWriter.Close()
		reqCancel()
		return nil, newStaleConnError(addr)
	}
	if reserver, ok := h.transport.(requestReserver); ok && !reserver.ReserveNewRequest() {
		_ = reqWriter.Close()
		reqCancel()
		return nil, newStaleConnError(addr)
	}

	// RoundTrip blocks on the response, and the server won't respond
	// until it reads the DialReq — so the body must be written from a
	// separate goroutine.
	go func() {
		_, err := protodelim.MarshalTo(reqWriter, dreq)
		if err != nil {
			_ = reqWriter.CloseWithError(err)
		}
	}()

	type roundTripResult struct {
		resp *http.Response
		err  error
	}
	roundTripCh := make(chan roundTripResult, 1)
	go func() {
		resp, err := h.transport.RoundTrip(req)
		roundTripCh <- roundTripResult{resp: resp, err: err}
	}()

	var resp *http.Response
	select {
	case result := <-roundTripCh:
		resp, err = result.resp, result.err
	case <-ctx.Done():
		_ = reqWriter.Close()
		reqCancel()
		return nil, &net.OpError{Op: "dial", Net: addr.Network(), Addr: addr, Err: ctx.Err()}
	}
	if err != nil {
		_ = reqWriter.Close()
		reqCancel()
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, &net.OpError{Op: "dial", Net: addr.Network(), Addr: addr, Err: err}
		}
		se := newStaleConnError(addr)
		se.wrapped = err
		return nil, se
	}
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		_ = reqWriter.Close()
		reqCancel()
		return nil, dialResponseError(resp, addr, string(snippet))
	}

	return &dialConn{
		remoteAddr: addr,
		reqBody:    reqWriter,
		respBody:   resp.Body,
		reqCancel:  reqCancel,
	}, nil
}

const dialErrorCodeHeader = "Ngrok-Error-Code"

// dialResponseError translates a non-200 /dial response into a
// *net.OpError that wraps a *ServerError carrying the structured ngrok
// error code (when the server sent one). The ServerError's Unwrap
// chain points at a familiar net sentinel — *net.DNSError for "not
// found" / 404, syscall.ECONNREFUSED for "no endpoint" / 503, etc. —
// so callers can use either errors.As(&serverErr) for the ngrok code
// or errors.Is(err, syscall.ECONNREFUSED) / errors.As(&dnsErr) for
// generic "couldn't connect" handling.
func dialResponseError(resp *http.Response, addr dialAddr, body string) error {
	body = strings.TrimSpace(body)
	code := resp.Header.Get(dialErrorCodeHeader)

	var sentinel error
	switch resp.StatusCode {
	case http.StatusNotFound:
		sentinel = &net.DNSError{Err: "no such host", Name: addr.host, IsNotFound: true}
	case http.StatusServiceUnavailable:
		sentinel = &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
		// Auth/quota/rate-limit rejections aren't routing failures, but
		// from the caller's POV "I tried to dial and the server said no"
		// behaves like ECONNREFUSED — a real connect that was refused.
		sentinel = &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}
	}

	server := &ServerError{
		Code:    code,
		Message: body,
		Status:  resp.StatusCode,
		wrapped: sentinel,
	}
	return &net.OpError{Op: "dial", Net: addr.Network(), Addr: addr, Err: server}
}

// ServerError carries the structured error a private-dial server returned
// on a non-200 /dial response. It is wrapped inside *net.OpError so
// callers who only care about the broad failure category can use
// errors.Is / errors.As against the standard net sentinels (e.g.
// *net.DNSError, syscall.ECONNREFUSED). Callers that want the ngrok
// error code or the server's human-readable message can extract it via
// errors.As(err, &serverErr).
type ServerError struct {
	// Code is the prefixed ngrok error code (e.g. "ERR_NGROK_706")
	// extracted from the Ngrok-Error-Code response header. May be
	// empty if the server did not emit one.
	Code string

	// Message is the server-provided human-readable description.
	Message string

	// Status is the HTTP status code of the response.
	Status int

	// wrapped is the net-style sentinel (*net.DNSError,
	// *os.SyscallError{ECONNREFUSED}, etc.) used for errors.Is /
	// errors.As composition. It is purely a type-match anchor; its
	// own Error() text is not included in ServerError.Error().
	wrapped error
}

func (e *ServerError) Error() string {
	switch {
	case e.Code != "" && e.Message != "":
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	case e.Code != "":
		return e.Code
	case e.Message != "":
		return e.Message
	default:
		return fmt.Sprintf("private-dial server returned %d", e.Status)
	}
}

// Unwrap returns the standard-library sentinel error chosen for this
// ServerError's status code, so errors.Is / errors.As walks down to it.
func (e *ServerError) Unwrap() error { return e.wrapped }

func (h *sessionConn) markClosed() {
	h.lifeMu.Lock()
	defer h.lifeMu.Unlock()
	h.lifeClosed = true
}

func (h *sessionConn) markDraining(grace time.Duration) {
	h.drainOnce.Do(func() {
		h.drainGrace = grace
		h.lifeMu.Lock()
		h.lifeDrained = true
		h.lifeMu.Unlock()
		close(h.drainCh)
	})
}

func (h *sessionConn) acceptsStreams() bool {
	h.lifeMu.Lock()
	defer h.lifeMu.Unlock()
	return !h.lifeClosed && !h.lifeDrained
}

func (h *sessionConn) close() error {
	h.closeOnce.Do(func() {
		h.markClosed()
		close(h.stopCh)
		if h.controlReqBody != nil {
			_ = h.controlReqBody.Close()
		}
		if h.controlRespBody != nil {
			_ = h.controlRespBody.Close()
		}
		if h.controlCancel != nil {
			h.controlCancel()
		}
		if h.transport != nil {
			if closer, ok := h.transport.(closeTransport); ok {
				_ = closer.Close()
			} else {
				h.transport.CloseIdleConnections()
			}
		}
	})
	return nil
}

// errSessionClosed is returned by sendControlFrame when the session has
// been closed or the controlSender goroutine has exited.
var errSessionClosed = errors.New("private-dial: session closed")

// sendControlFrame hands frame to the controlSender goroutine. It
// respects ctx, so a stalled wire (which can park the underlying
// pipe write indefinitely — see controlSender) doesn't pin the
// caller. The actual proto write happens asynchronously; a nil
// return means the frame was enqueued, not that bytes hit the wire.
func (h *sessionConn) sendControlFrame(ctx context.Context, frame *pbpd.ControlFrame) error {
	select {
	case h.sendCh <- frame:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-h.stopCh:
		return errSessionClosed
	case <-h.sendDone:
		return errSessionClosed
	}
}

// controlSender owns all writes to controlReqBody. Centralizing the
// pipe writes here lets sendControlFrame callers select on context,
// since pipe.Write itself has no context and can stall indefinitely
// when the peer stops reading. A terminal write error is forwarded
// to serverErrCh and the goroutine exits, closing sendDone so
// queued senders unblock.
func (h *sessionConn) controlSender() {
	defer close(h.sendDone)
	for {
		select {
		case <-h.stopCh:
			return
		case frame := <-h.sendCh:
			if _, err := protodelim.MarshalTo(h.controlReqBody, frame); err != nil {
				select {
				case h.serverErrCh <- err:
				default:
				}
				return
			}
		}
	}
}

// pingLoop sends a Ping every pingInterval and records RTT when the
// matching Pong arrives. It stops on Close.
//
// Each tick uses a per-send timeout of pingInterval so a stuck sender
// can't pin successive ticks; transient timeouts just skip a ping and
// let the next tick try again. Terminal errSessionClosed exits the loop.
func (h *sessionConn) pingLoop() {
	tick := time.NewTicker(h.pingInterval)
	defer tick.Stop()
	for {
		select {
		case <-h.stopCh:
			return
		case <-tick.C:
			token := h.recordPingSent(time.Now())
			ctx, cancel := context.WithTimeout(context.Background(), h.pingInterval)
			err := h.sendControlFrame(ctx, &pbpd.ControlFrame{
				Frame: &pbpd.ControlFrame_Ping{Ping: &pbpd.Ping{Token: token}},
			})
			cancel()
			if errors.Is(err, errSessionClosed) {
				return
			}
		}
	}
}

func (h *sessionConn) recordPingSent(now time.Time) uint64 {
	var buf [8]byte
	_, _ = cryptorand.Read(buf[:])
	token := binary.LittleEndian.Uint64(buf[:])
	h.pingsMu.Lock()
	h.pings[token] = now
	h.pingsMu.Unlock()
	return token
}

func (h *sessionConn) completePing(token uint64, now time.Time) (time.Duration, bool) {
	h.pingsMu.Lock()
	defer h.pingsMu.Unlock()
	sent, ok := h.pings[token]
	if !ok {
		return 0, false
	}
	delete(h.pings, token)
	return now.Sub(sent), true
}

// readControl pumps ControlFrames from the server until EOF or error.
// It closes drainCh on PleaseDrain, echoes Pong on inbound Ping (so the
// server can measure its RTT to us), records client-side RTT on Pong,
// and forwards terminal errors to serverErrCh.
func (h *sessionConn) readControl() {
	defer func() {
		select {
		case h.serverErrCh <- io.EOF:
		default:
		}
	}()
	for {
		frame := new(pbpd.ControlFrame)
		if err := protodelimUnmarshaler.UnmarshalFrom(h.controlRespBody, frame); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return
			}
			select {
			case h.serverErrCh <- err:
			default:
			}
			return
		}
		switch f := frame.Frame.(type) {
		case *pbpd.ControlFrame_PleaseDrain:
			h.markDraining(time.Duration(f.PleaseDrain.GetGracePeriodSeconds()) * time.Second)
		case *pbpd.ControlFrame_SessionError:
			select {
			case h.serverErrCh <- fmt.Errorf("server session error: %s", f.SessionError.GetMessage()):
			default:
			}
			return
		case *pbpd.ControlFrame_Ping:
			_ = h.sendControlFrame(context.Background(), &pbpd.ControlFrame{
				Frame: &pbpd.ControlFrame_Pong{Pong: &pbpd.Pong{Token: f.Ping.GetToken()}},
			})
		case *pbpd.ControlFrame_Pong:
			if rtt, ok := h.completePing(f.Pong.GetToken(), time.Now()); ok {
				h.rttMu.Lock()
				h.lastRTT = rtt
				h.rttMu.Unlock()
			}
		}
	}
}

// dialConn is the net.Conn returned from Session.DialContext. Read pulls
// from the response body; Write pushes into the request body. Close terminates
// both sides. Dial-level failures never reach a dialConn — they're returned
// from DialContext directly as *net.OpError. EOF on Read surfaces normally as
// io.EOF.
type dialConn struct {
	reqBody   *io.PipeWriter
	respBody  io.ReadCloser
	reqCancel context.CancelFunc

	remoteAddr dialAddr

	closeOnce sync.Once
}

func (c *dialConn) Read(p []byte) (int, error)  { return c.respBody.Read(p) }
func (c *dialConn) Write(p []byte) (int, error) { return c.reqBody.Write(p) }

// CloseWrite signals end-of-request to the server by closing the request
// body. The response side stays open so the caller can keep reading.
func (c *dialConn) CloseWrite() error {
	return c.reqBody.Close()
}

func (c *dialConn) Close() error {
	c.closeOnce.Do(func() {
		_ = c.reqBody.Close()
		_ = c.respBody.Close()
		if c.reqCancel != nil {
			c.reqCancel()
		}
	})
	return nil
}

func (c *dialConn) LocalAddr() net.Addr                { return dialAddr{} }
func (c *dialConn) RemoteAddr() net.Addr               { return c.remoteAddr }
func (c *dialConn) SetDeadline(_ time.Time) error      { return nil }
func (c *dialConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *dialConn) SetWriteDeadline(_ time.Time) error { return nil }

// dialAddr implements net.Addr.
// It is specialized to the 'tcp' network since that's all we support for
// private endpoints currently.
type dialAddr struct {
	addr string // host:port
	host string
	port int
}

func (a dialAddr) Network() string { return "tcp" }
func (a dialAddr) String() string  { return a.addr }
