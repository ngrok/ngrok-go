// Package privatedial implements a client to allow dialing private ngrok
// endpoints. It authenticates with an API Key as its authtoken and then multiplexes
// per-target net.Conn streams over a single HTTP/2 or HTTP/3
// connection.
package privatedial

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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
	// dialTimeout bounds a single connection attempt during the race.
	dialTimeout = 3 * time.Second
	// quicHeadStart is how long the QUIC attempt runs alone before the
	// HTTP/2 attempt is staggered in. If QUIC completes within this window
	// no second connection is ever made.
	quicHeadStart = 250 * time.Millisecond
)

// stickyProtocol records the first protocol the race settled on in this
// process. Once set, OpenSession reuses it for every subsequent session
// (including reconnects) and skips the happy-eyeballs race entirely.
var (
	stickyMu       sync.Mutex
	stickyProtocol = ProtocolAuto
)

func setStickyProtocol(p Protocol) {
	stickyMu.Lock()
	if stickyProtocol == ProtocolAuto {
		stickyProtocol = p
	}
	stickyMu.Unlock()
}

func getStickyProtocol() Protocol {
	stickyMu.Lock()
	defer stickyMu.Unlock()
	return stickyProtocol
}

// roundTripCloser is the subset of *http2.Transport / *http3.Transport the
// session relies on. Holding the transport behind this interface lets a
// single Session implementation drive either protocol.
type roundTripCloser interface {
	RoundTrip(*http.Request) (*http.Response, error)
	CloseIdleConnections()
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
// caller must Close the Session to release the connection.
//
// Transport selection follows the spec's Happy-Eyeballs-like algorithm: by
// default it races HTTP/3 (QUIC) against HTTP/2, preferring QUIC, and
// remembers the winning protocol process-wide so later sessions skip the
// race. ClientOpts.ForceProtocol overrides this.
func (c *Client) OpenSession(ctx context.Context) (*Session, error) {
	proto := c.opts.ForceProtocol
	if proto == ProtocolAuto {
		proto = getStickyProtocol()
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
			setStickyProtocol(ProtocolQUIC)
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
				closeLoser(h2Ch, h2Cancel)
				setStickyProtocol(ProtocolQUIC)
				return r.sess, nil
			}
			quicErr = r.err
		case r := <-h2Ch:
			h2Done = true
			if r.err == nil {
				h2Cancel()
				closeLoser(quicCh, quicCancel)
				setStickyProtocol(ProtocolH2)
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
	var (
		sess *Session
		err  error
	)
	switch p {
	case ProtocolQUIC:
		sess, err = c.openSessionWith(ctx, c.newH3Transport(), c.opts.QUICServerAddr)
	case ProtocolH2:
		sess, err = c.openSessionWith(ctx, c.newH2Transport(), c.opts.H2ServerAddr)
	default:
		return nil, fmt.Errorf("private-dial: unsupported protocol %d", p)
	}
	if err != nil {
		return nil, err
	}
	sess.proto = p
	return sess, nil
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

// newH2Transport builds the HTTP/2 transport.
//
// A single *http2.Transport pinned to a single net.Conn. http2 will
// multiplex all streams over that one conn, which matches the server's
// per-conn session model.
//
// ReadIdleTimeout + PingTimeout enable h2 keepalive PINGs so a dead
// peer is detected at the transport layer. Without this, a stalled
// peer can park indefinite Writes on the request body pipe (see
// sendControlFrame). With keepalive, h2 closes the conn on missed
// PING, which closes the body reader and unblocks stuck writers.
func (c *Client) newH2Transport() roundTripCloser {
	return &http2.Transport{
		TLSClientConfig: c.tlsConfigFor(c.opts.H2ServerAddr, "h2"),
		AllowHTTP:       false,
		ReadIdleTimeout: 30 * time.Second,
		PingTimeout:     15 * time.Second,
	}
}

// newH3Transport builds the HTTP/3 (QUIC) transport. KeepAlivePeriod is the
// QUIC analogue of the h2 keepalive PINGs: it keeps the single multiplexing
// connection alive and surfaces a dead peer to stuck stream writers.
func (c *Client) newH3Transport() roundTripCloser {
	return &http3.Transport{
		TLSClientConfig: c.tlsConfigFor(c.opts.QUICServerAddr, "h3"),
		QUICConfig: &quic.Config{
			KeepAlivePeriod: 30 * time.Second,
		},
	}
}

// openSessionWith runs the /session handshake over transport (reaching
// serverAddr) and returns the authenticated Session.
func (c *Client) openSessionWith(ctx context.Context, transport roundTripCloser, serverAddr string) (*Session, error) {
	controlURL := &url.URL{Scheme: "https", Host: serverAddr, Path: "/session"}
	dialURL := &url.URL{Scheme: "https", Host: serverAddr, Path: "/dial"}

	// io.Pipe for the request body — first frame is SessionReq, then
	// the body is reused for client-→server ControlFrames (Ping/Pong).
	bodyReader, bodyWriter := io.Pipe()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, controlURL.String(), bodyReader)
	if err != nil {
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

	resp, err := transport.RoundTrip(req)
	if err != nil {
		_ = bodyWriter.Close()
		transport.CloseIdleConnections()
		return nil, fmt.Errorf("private-dial /session: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		_ = bodyWriter.Close()
		transport.CloseIdleConnections()
		return nil, fmt.Errorf("private-dial /session status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	respBody := newReadCloseByteReader(resp.Body)
	resp.Body = respBody

	ack := new(pbpd.SessionAck)
	if err := protodelimUnmarshaler.UnmarshalFrom(respBody, ack); err != nil {
		_ = resp.Body.Close()
		_ = bodyWriter.Close()
		transport.CloseIdleConnections()
		return nil, fmt.Errorf("read SessionAck: %w", err)
	}

	sess := &Session{
		serverID:        ack.GetServerId(),
		pingInterval:    ack.GetPingInterval().AsDuration(),
		transport:       transport,
		dialURL:         dialURL,
		authToken:       c.opts.AuthToken,
		sessionReq:      &pbpd.SessionReq{ClientVersion: c.opts.ClientVersion, Metadata: c.opts.Metadata},
		controlRespBody: respBody,
		controlReqBody:  bodyWriter,
		sendCh:          make(chan *pbpd.ControlFrame),
		sendDone:        make(chan struct{}),
		pings:           map[uint64]time.Time{},
		drainCh:         make(chan struct{}),
		serverErrCh:     make(chan error, 1),
		stopCh:          make(chan struct{}),
	}
	// /session has acked, so we already received a server response. Any
	// subsequent /dial on this session can drop the embedded SessionReq.
	sess.responseReceived.Store(true)
	go sess.controlSender()
	go sess.readControl()
	if sess.pingInterval > 0 {
		go sess.pingLoop()
	}

	return sess, nil
}

// Session represents an authenticated private-dial session. It multiplexes
// Dial calls over a single HTTP/2 connection.
type Session struct {
	serverID     string
	pingInterval time.Duration

	// proto is the transport this session settled on (ProtocolQUIC or
	// ProtocolH2). Set once at open time.
	proto Protocol

	transport roundTripCloser
	dialURL   *url.URL
	authToken string

	// sessionReq is embedded in DialReq.session_req on /dial requests
	// until responseReceived is true, so that a /dial opened in parallel
	// with /session can self-authenticate server-side without waiting
	// for SessionAck.
	sessionReq       *pbpd.SessionReq
	responseReceived atomic.Bool

	controlRespBody *readCloseByteReader
	controlReqBody  *io.PipeWriter

	// sendCh hands ControlFrames to the controlSender goroutine, which
	// owns all writes to controlReqBody. Producers select on ctx/stopCh/
	// sendDone alongside the send so a stalled wire never pins them.
	sendCh   chan *pbpd.ControlFrame
	sendDone chan struct{}

	// pings tracks outstanding client-→server pings keyed by their
	// random 8-byte token, used to compute client-side RTT on Pong.
	pingsMu sync.Mutex
	pings   map[uint64]time.Time

	drainOnce   sync.Once
	drainCh     chan struct{}
	serverErrCh chan error

	// stopCh is closed by Close to halt the pingLoop and controlSender
	// goroutines.
	stopCh chan struct{}

	// LastRTT is the most recent client-side ping round-trip time. Read
	// it via LastRTT(); reset to zero before any successful pong arrives.
	rttMu   sync.Mutex
	lastRTT time.Duration

	closeOnce sync.Once
	closeErr  error
}

// ServerID returns the opaque identifier the server emitted in SessionAck.
// Useful for log correlation across reconnects.
func (s *Session) ServerID() string { return s.serverID }

// Protocol returns the transport this session settled on — ProtocolQUIC
// (HTTP/3) or ProtocolH2 (HTTP/2).
func (s *Session) Protocol() Protocol { return s.proto }

// PingInterval is the cadence at which the server expects to send and
// receive Ping frames on the control stream.
func (s *Session) PingInterval() time.Duration { return s.pingInterval }

// LastRTT returns the most recent round-trip time measured by the
// client-side ping loop. Zero before the first pong arrives.
func (s *Session) LastRTT() time.Duration {
	s.rttMu.Lock()
	defer s.rttMu.Unlock()
	return s.lastRTT
}

// DrainCh is closed when the server sends a PleaseDrain control frame. Callers
// should stop issuing new Dial calls once this fires.
func (s *Session) DrainCh() <-chan struct{} { return s.drainCh }

// ServerErrCh delivers, at most once, an error from the control stream (I/O
// failure, explicit SessionError frame, or clean EOF as io.EOF). After a
// value is delivered the session is effectively dead.
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
	rAddr := dialAddr{addr: addr, host: host}

	reqReader, reqWriter := io.Pipe()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.dialURL.String(), reqReader)
	if err != nil {
		_ = reqWriter.Close()
		return nil, &net.OpError{Op: "dial", Net: network, Addr: rAddr, Err: fmt.Errorf("build /dial request: %w", err)}
	}
	req.Header.Set("Authorization", "Bearer "+s.authToken)
	req.Header.Set("Content-Type", "application/octet-stream")

	dreq := &pbpd.DialReq{
		Host: host,
		Port: int64(port),
	}
	// Until we've seen any response from the server, embed SessionReq so
	// that /dial can authenticate on its own when raced ahead of /session.
	if !s.responseReceived.Load() {
		dreq.SessionReq = s.sessionReq
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

	resp, err := s.transport.RoundTrip(req)
	if err != nil {
		_ = reqWriter.Close()
		return nil, &net.OpError{Op: "dial", Net: network, Addr: rAddr, Err: err}
	}
	// Any successful response from the server means our auth went through.
	s.responseReceived.Store(true)
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		_ = reqWriter.Close()
		return nil, dialResponseError(resp, rAddr, string(snippet))
	}

	return &dialConn{
		remoteAddr: rAddr,
		reqBody:    reqWriter,
		respBody:   resp.Body,
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

// Close tears down the control stream and the underlying HTTP/2 connection.
// Any in-flight Dial streams will see read/write errors.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		close(s.stopCh)
		_ = s.controlReqBody.Close()
		if s.controlRespBody != nil {
			_ = s.controlRespBody.Close()
		}
		s.transport.CloseIdleConnections()
	})
	return s.closeErr
}

// errSessionClosed is returned by sendControlFrame when the session has
// been closed or the controlSender goroutine has exited.
var errSessionClosed = errors.New("private-dial: session closed")

// sendControlFrame hands frame to the controlSender goroutine. It
// respects ctx, so a stalled wire (which can park the underlying
// pipe write indefinitely — see controlSender) doesn't pin the
// caller. The actual proto write happens asynchronously; a nil
// return means the frame was enqueued, not that bytes hit the wire.
func (s *Session) sendControlFrame(ctx context.Context, frame *pbpd.ControlFrame) error {
	select {
	case s.sendCh <- frame:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.stopCh:
		return errSessionClosed
	case <-s.sendDone:
		return errSessionClosed
	}
}

// controlSender owns all writes to controlReqBody. Centralizing the
// pipe writes here lets sendControlFrame callers select on context,
// since pipe.Write itself has no context and can stall indefinitely
// when the peer stops reading. A terminal write error is forwarded
// to serverErrCh and the goroutine exits, closing sendDone so
// queued senders unblock.
func (s *Session) controlSender() {
	defer close(s.sendDone)
	for {
		select {
		case <-s.stopCh:
			return
		case frame := <-s.sendCh:
			if _, err := protodelim.MarshalTo(s.controlReqBody, frame); err != nil {
				select {
				case s.serverErrCh <- err:
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
func (s *Session) pingLoop() {
	tick := time.NewTicker(s.pingInterval)
	defer tick.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-tick.C:
			token := s.recordPingSent(time.Now())
			ctx, cancel := context.WithTimeout(context.Background(), s.pingInterval)
			err := s.sendControlFrame(ctx, &pbpd.ControlFrame{
				Frame: &pbpd.ControlFrame_Ping{Ping: &pbpd.Ping{Token: token}},
			})
			cancel()
			if errors.Is(err, errSessionClosed) {
				return
			}
		}
	}
}

func (s *Session) recordPingSent(now time.Time) uint64 {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	token := binary.LittleEndian.Uint64(buf[:])
	s.pingsMu.Lock()
	s.pings[token] = now
	s.pingsMu.Unlock()
	return token
}

func (s *Session) completePing(token uint64, now time.Time) (time.Duration, bool) {
	s.pingsMu.Lock()
	defer s.pingsMu.Unlock()
	sent, ok := s.pings[token]
	if !ok {
		return 0, false
	}
	delete(s.pings, token)
	return now.Sub(sent), true
}

// readControl pumps ControlFrames from the server until EOF or error.
// It closes drainCh on PleaseDrain, echoes Pong on inbound Ping (so the
// server can measure its RTT to us), records client-side RTT on Pong,
// and forwards terminal errors to serverErrCh.
func (s *Session) readControl() {
	defer func() {
		select {
		case s.serverErrCh <- io.EOF:
		default:
		}
	}()
	for {
		frame := new(pbpd.ControlFrame)
		if err := protodelimUnmarshaler.UnmarshalFrom(s.controlRespBody, frame); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return
			}
			select {
			case s.serverErrCh <- err:
			default:
			}
			return
		}
		switch f := frame.Frame.(type) {
		case *pbpd.ControlFrame_PleaseDrain:
			s.drainOnce.Do(func() { close(s.drainCh) })
		case *pbpd.ControlFrame_SessionError:
			select {
			case s.serverErrCh <- fmt.Errorf("server session error: %s", f.SessionError.GetMessage()):
			default:
			}
			return
		case *pbpd.ControlFrame_Ping:
			_ = s.sendControlFrame(context.Background(), &pbpd.ControlFrame{
				Frame: &pbpd.ControlFrame_Pong{Pong: &pbpd.Pong{Token: f.Ping.GetToken()}},
			})
		case *pbpd.ControlFrame_Pong:
			if rtt, ok := s.completePing(f.Pong.GetToken(), time.Now()); ok {
				s.rttMu.Lock()
				s.lastRTT = rtt
				s.rttMu.Unlock()
			}
		}
	}
}

// dialConn is the net.Conn returned from Session.DialContext. Read pulls
// from the HTTP/2 response body; Write pushes into the HTTP/2 request body.
// Close terminates both sides. Dial-level failures never reach a dialConn —
// they're returned from DialContext directly as *net.OpError. EOF on Read
// surfaces normally as io.EOF.
type dialConn struct {
	reqBody  *io.PipeWriter
	respBody io.ReadCloser

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
}

func (a dialAddr) Network() string { return "tcp" }
func (a dialAddr) String() string  { return a.addr }
