// Package privatedial implements a client to allow dialing private ngrok
// endpoints. It authenticates with an API Key as its authtoken and then multiplexes
// per-target net.Conn streams over a single HTTP/2 or HTTP/3
// connection. A Dialer connects lazily on first use and transparently
// reconnects on server drain or abrupt control-stream failure; new Dial
// calls follow the freshest underlying transport while in-flight streams
// ride out the server-advertised drain grace period on the old one.
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
	"log/slog"
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

	pbpd "golang.ngrok.com/ngrok/privatedial/internal/pb_private_dial"
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

	h2ReadIdleTimeout = 10 * time.Second
	h2PingTimeout     = 5 * time.Second

	quicKeepAlivePeriod = h2ReadIdleTimeout
	quicMaxIdleTimeout  = h2ReadIdleTimeout + h2PingTimeout
)

var errDialWaitTimeout = errors.New("private-dial: dial wait timeout")

// stickyProtocol records the first protocol the race settled on in this
// process. Once set, every subsequent connect (including reconnects) reuses
// it and skips the happy-eyeballs race entirely.
var stickyProtocol atomic.Pointer[Protocol]

// roundTripCloser is the subset of transport behavior the session relies on.
// Holding the transport behind this interface lets a single Dialer
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

// Config configures a Dialer. The server addresses are endpoints serving the
// private-dial protocol; the net.Dial to them must reach a mux
// PrivateDialIngresses listener.
type Config struct {
	// QUICServerAddr is the "host:port" used for the HTTP/3 (QUIC)
	// transport (e.g. "quic.connect-endpoint.ngrok.com:443"). Required
	// unless ForceProtocol is ProtocolH2.
	QUICServerAddr string

	// H2ServerAddr is the "host:port" used for the HTTP/2 transport
	// (e.g. "h2.connect-endpoint.ngrok.com:443"). Required unless
	// ForceProtocol is ProtocolQUIC.
	H2ServerAddr string

	// ForceProtocol, when not ProtocolAuto, skips the Happy-Eyeballs race
	// and uses only the named transport. Useful when the caller knows that
	// UDP (force ProtocolH2) or TCP (force ProtocolQUIC) is unavailable.
	ForceProtocol Protocol

	// AuthToken is the auth token to use. During development, this is an ngrok
	// API Key, it'll be a proper token eventually.
	AuthToken string

	// TLSConfig overrides the default TLS config. Its ServerName, when set,
	// overrides the SNI, which otherwise defaults to the host portion of
	// the per-protocol server address. MinVersion defaults to TLS 1.3 when
	// unset, and NextProtos is always set to match the negotiated transport
	// (h2 or h3).
	TLSConfig *tls.Config

	// ClientVersion is metadata about this client.
	ClientVersion string

	// Metadata is arbitrary metadata about this client. This will be made
	// available (TODO) in the cel environment of endpoints receiving requests
	// from this client.
	Metadata map[string]string

	// Logger, when set, receives debug-level logs of connection lifecycle
	// events: protocol selection, session establishment, reconnects, drains,
	// and per-stream dials. A nil Logger disables logging.
	Logger *slog.Logger
}

// New returns a Dialer for the configured server. It does not open any
// connection: the first DialContext establishes one, or call Connect to
// establish it eagerly. The caller must Close the Dialer to release the
// connection.
func New(cfg Config) *Dialer {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Dialer{
		cfg:    cfg,
		log:    logger,
		ctx:    ctx,
		cancel: cancel,
		ready:  make(chan struct{}),
		done:   make(chan struct{}),
	}
}

// serverNameFor returns the default SNI for a server address: its host
// portion. Callers override it via Config.TLSConfig.ServerName.
func serverNameFor(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// open establishes the initial connection to the server, opens the control
// stream (/session), and authenticates, returning the conn and the protocol
// it settled on.
//
// Transport selection follows the spec's Happy-Eyeballs-like algorithm: by
// default it races HTTP/3 (QUIC) against HTTP/2, preferring QUIC, and
// remembers the winning protocol process-wide so later connects skip the
// race. Config.ForceProtocol overrides this.
func (d *Dialer) open(ctx context.Context) (*sessionConn, Protocol, error) {
	proto := d.cfg.ForceProtocol
	if proto == ProtocolAuto {
		stickyProto := stickyProtocol.Load()
		if stickyProto != nil {
			proto = *stickyProto
			d.log.Debug("reusing protocol from earlier race", "protocol", proto)
		}
	}
	switch proto {
	case ProtocolQUIC, ProtocolH2:
		// Forced, or a previous race already settled on a protocol.
		conn, err := d.openConn(ctx, proto)
		return conn, proto, err
	default:
		d.log.Debug("racing HTTP/3 against HTTP/2", "quic_head_start", quicHeadStart)
		return d.race(ctx)
	}
}

// race implements the spec's staggered Happy-Eyeballs algorithm: start the
// QUIC attempt, give it a head start, then stagger in the HTTP/2 attempt;
// the first to produce a usable conn wins (QUIC preferred), the loser is
// cancelled and any conn it raced through is closed.
func (d *Dialer) race(ctx context.Context) (*sessionConn, Protocol, error) {
	type result struct {
		proto Protocol
		conn  *sessionConn
		err   error
	}

	launch := func(p Protocol) (chan result, context.CancelFunc) {
		attemptCtx, cancel := context.WithTimeout(ctx, dialTimeout)
		ch := make(chan result, 1)
		go func() {
			conn, err := d.openConn(attemptCtx, p)
			if err != nil {
				d.log.Debug("race attempt failed", "protocol", p, "error", err)
			}
			ch <- result{proto: p, conn: conn, err: err}
		}()
		return ch, cancel
	}

	// closeLoser cancels a still-running attempt and closes the conn it
	// produced if it happened to win the race after we'd already committed.
	closeLoser := func(ch chan result, cancel context.CancelFunc) {
		cancel()
		go func() {
			if r := <-ch; r.conn != nil {
				_ = r.conn.close()
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
			d.log.Debug("race settled within head start", "protocol", ProtocolQUIC)
			return r.conn, ProtocolQUIC, nil
		}
		quicErr = r.err
	case <-timer.C:
		// QUIC still in flight; stagger in HTTP/2 below.
		d.log.Debug("HTTP/3 head start elapsed, staggering in HTTP/2")
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
				d.log.Debug("race settled", "protocol", ProtocolQUIC)
				return r.conn, ProtocolQUIC, nil
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
				d.log.Debug("race settled", "protocol", ProtocolH2)
				return r.conn, ProtocolH2, nil
			}
			h2Err = r.err
		}
	}

	quicCancel()
	h2Cancel()
	return nil, ProtocolAuto, bothTransportsFailed(quicErr, h2Err)
}

// bothTransportsFailed combines the per-transport failures from the race into
// a single error. When both transports failed with the same ngrok error code,
// the server reported one underlying cause to both attempts, so the result
// collapses to that error rather than the redundant "quic=X, h2=X" form.
func bothTransportsFailed(quicErr, h2Err error) error {
	var qe, he Error
	if errors.As(quicErr, &qe) && errors.As(h2Err, &he) &&
		qe.Code() != "" && qe.Code() == he.Code() {
		return qe
	}
	return fmt.Errorf("private-dial: both transports failed: quic=%v, h2=%v", quicErr, h2Err)
}

func (d *Dialer) openConn(ctx context.Context, p Protocol) (*sessionConn, error) {
	var (
		transport  roundTripCloser
		serverAddr string
		remoteAddr string
		rec        *connAddrRecorder
		err        error
	)
	switch p {
	case ProtocolQUIC:
		if d.cfg.QUICServerAddr == "" {
			return nil, errors.New("private-dial: Config.QUICServerAddr is required for HTTP/3")
		}
		transport, rec = d.newH3Transport()
		serverAddr = d.cfg.QUICServerAddr
	case ProtocolH2:
		if d.cfg.H2ServerAddr == "" {
			return nil, errors.New("private-dial: Config.H2ServerAddr is required for HTTP/2")
		}
		transport, remoteAddr, err = d.newH2Transport(ctx)
		if err != nil {
			return nil, err
		}
		serverAddr = d.cfg.H2ServerAddr
	default:
		return nil, fmt.Errorf("private-dial: unsupported protocol %d", p)
	}

	d.log.Debug("opening session", "protocol", p, "server_addr", serverAddr)

	h := &sessionConn{
		log:        d.log.With("protocol", p),
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
	req.Header.Set("Authorization", "Bearer "+d.cfg.AuthToken)
	req.Header.Set("Content-Type", "application/x-protobuf")
	if d.cfg.ClientVersion != "" {
		req.Header.Set("User-Agent", d.cfg.ClientVersion)
	}

	// Write SessionReq concurrently with RoundTrip — the server reads
	// it before sending headers, so RoundTrip won't return until this
	// hits the wire.
	go func() {
		_, err := protodelim.MarshalTo(bodyWriter, &pbpd.SessionReq{
			ClientVersion: d.cfg.ClientVersion,
			Metadata:      d.cfg.Metadata,
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
		// The server can reject a session by committing to a 200 and then
		// reporting the failure via trailers instead of sending a
		// SessionAck. Because this read is synchronous, that error surfaces
		// straight out of the initial Connect rather than later via Done/Err.
		if rerr := errorFromTrailer(resp.Trailer); rerr != nil {
			return nil, rerr
		}
		return nil, fmt.Errorf("read SessionAck: %w", err)
	}

	if rec != nil {
		h.remoteAddr = rec.get()
	}
	h.serverID = ack.GetServerId()
	h.log = h.log.With("server_id", h.serverID)
	h.pingInterval = ack.GetPingInterval().AsDuration()
	h.dialURL = dialURL
	h.authToken = d.cfg.AuthToken
	h.controlResp = resp
	h.controlRespBody = respBody
	h.sendCh = make(chan *pbpd.ControlFrame)
	h.sendDone = make(chan struct{})
	h.pings = map[uint64]time.Time{}
	h.drainCh = make(chan struct{})
	h.serverErrCh = make(chan error, 1)

	h.log.Debug("session established",
		"remote_addr", h.remoteAddr,
		"ping_interval", h.pingInterval,
	)

	go h.controlSender()
	go h.readControl()
	if h.pingInterval > 0 {
		go h.pingLoop()
	}
	return h, nil
}

// tlsConfigFor clones the configured TLS settings and pins them for the
// given server address and ALPN protocol.
func (d *Dialer) tlsConfigFor(serverAddr string, nextProtos ...string) *tls.Config {
	tlsCfg := &tls.Config{}
	if d.cfg.TLSConfig != nil {
		tlsCfg = d.cfg.TLSConfig.Clone()
	}
	if tlsCfg.ServerName == "" {
		tlsCfg.ServerName = serverNameFor(serverAddr)
	}
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
func (d *Dialer) newH2Transport(ctx context.Context) (roundTripCloser, string, error) {
	tlsConn, err := (&tls.Dialer{Config: d.tlsConfigFor(d.cfg.H2ServerAddr, "h2")}).
		DialContext(ctx, "tcp", d.cfg.H2ServerAddr)
	if err != nil {
		return nil, "", fmt.Errorf("private-dial: tls dial: %w", err)
	}
	remoteAddr := tlsConn.RemoteAddr().String()
	transport := &http2.Transport{
		ReadIdleTimeout: h2ReadIdleTimeout,
		PingTimeout:     h2PingTimeout,
	}
	cc, err := transport.NewClientConn(tlsConn)
	if err != nil {
		_ = tlsConn.Close()
		return nil, "", fmt.Errorf("private-dial: h2 client conn: %w", err)
	}
	return &h2ClientConnTransport{cc: cc}, remoteAddr, nil
}

// newH3Transport builds the HTTP/3 (QUIC) transport and records the server
// address the QUIC connection lands on. QUIC does not expose separate read-idle
// and ping timeouts like HTTP/2, so bound its idle timeout and send keepalives
// on the same cadence as h2's read-idle probes.
func (d *Dialer) newH3Transport() (roundTripCloser, *connAddrRecorder) {
	rec := &connAddrRecorder{}
	t := &http3.Transport{
		TLSClientConfig: d.tlsConfigFor(d.cfg.QUICServerAddr, "h3"),
		QUICConfig: &quic.Config{
			KeepAlivePeriod: quicKeepAlivePeriod,
			MaxIdleTimeout:  quicMaxIdleTimeout,
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

// Dialer dials private ngrok endpoints over a single authenticated
// private-dial session. It connects lazily on the first DialContext (or
// eagerly via Connect), reconnects on server drain and control-stream
// failures, and routes new Dial calls to the freshest underlying transport.
type Dialer struct {
	cfg Config
	log *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc

	// openFn opens replacement per-transport connections. It is set when
	// the initial connect settles on a protocol, before the supervisor
	// starts, and never mutated afterwards.
	openFn func(context.Context) (*sessionConn, error)

	clock sessionClock

	drainGroup sync.WaitGroup

	mu sync.Mutex
	// connecting tracks an in-flight initial connect so concurrent callers
	// coalesce onto a single handshake.
	connecting *connectAttempt
	// connected is set once the initial connect has succeeded and the
	// reconnect supervisor owns the connection.
	connected bool
	// proto is the transport this dialer settled on (ProtocolQUIC or
	// ProtocolH2). ProtocolAuto until the initial connect completes.
	proto    Protocol
	current  *sessionConn
	fatalErr error
	// ready is closed every time current is replaced or fatalErr is set.
	ready chan struct{}

	dialWait time.Duration

	// done is closed (via doneOnce) when the Dialer is permanently done
	// dialing: on Close, or on a fatal session error.
	doneOnce sync.Once
	done     chan struct{}

	closeOnce sync.Once
}

// connectAttempt is a single in-flight initial connect. err is only valid
// once done is closed.
type connectAttempt struct {
	done chan struct{}
	err  error
}

// Connect forces the Dialer to establish its connection to the server,
// returning any fatal authentication or session errors. It is safe to call
// concurrently and repeatedly: concurrent calls coalesce onto a single
// handshake, an already-connected Dialer waits only for a usable connection
// (immediate unless a reconnect is in flight), and a Dialer whose initial
// connect failed retries it. Calling Connect is optional — DialContext
// connects on first use.
//
// ctx bounds only this call. Once established, the connection is scoped to
// the Dialer and is released by Close.
func (d *Dialer) Connect(ctx context.Context) error {
	if err := d.ensureConnected(ctx); err != nil {
		return err
	}
	_, err := d.waitForCurrent(ctx)
	return err
}

// ensureConnected runs the initial connect, coalescing concurrent callers
// onto a single handshake. A failed attempt leaves the Dialer unconnected so
// a later call can retry. Once an attempt succeeds the reconnect supervisor
// owns the connection and ensureConnected returns nil without waiting.
func (d *Dialer) ensureConnected(ctx context.Context) error {
	d.mu.Lock()
	if d.connected {
		d.mu.Unlock()
		return nil
	}
	if d.ctx.Err() != nil {
		d.mu.Unlock()
		return ErrSessionClosed
	}
	if a := d.connecting; a != nil {
		d.mu.Unlock()
		select {
		case <-a.done:
			return a.err
		case <-ctx.Done():
			return ctx.Err()
		case <-d.ctx.Done():
			return ErrSessionClosed
		}
	}
	a := &connectAttempt{done: make(chan struct{})}
	d.connecting = a
	d.mu.Unlock()

	first, proto, err := d.open(ctx)

	d.mu.Lock()
	d.connecting = nil
	if err == nil && d.ctx.Err() != nil {
		// Closed while the handshake was in flight.
		_ = first.close()
		err = ErrSessionClosed
	}
	if err == nil {
		d.proto = proto
		d.openFn = func(ctx context.Context) (*sessionConn, error) {
			return d.openConn(ctx, proto)
		}
		d.current = first
		d.connected = true
		d.signalReadyLocked()
		go d.supervise()
	}
	a.err = err
	close(a.done)
	d.mu.Unlock()
	if err != nil {
		d.log.Debug("initial connect failed", "error", err)
	}
	return err
}

const defaultDialWait = 5 * time.Second

// ServerID returns the opaque identifier the server emitted in the most recent
// SessionAck, or the empty string if no conn is currently established.
func (d *Dialer) ServerID() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current == nil {
		return ""
	}
	return d.current.serverID
}

// Protocol returns the transport this dialer settled on — ProtocolQUIC
// (HTTP/3) or ProtocolH2 (HTTP/2) — or ProtocolAuto if it has not yet
// connected.
func (d *Dialer) Protocol() Protocol {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.proto
}

// RemoteAddr returns the server address (host:port) the current underlying
// transport connection landed on. It returns an empty string if no connection
// is currently established.
func (d *Dialer) RemoteAddr() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current == nil {
		return ""
	}
	return d.current.remoteAddr
}

// Done returns a channel that is closed when the Dialer is permanently done
// dialing: after Close, or after a fatal session error such as a
// non-retryable auth failure during reconnect. Err reports why. Transient
// control-stream failures are consumed by the reconnect supervisor and do
// not close the channel.
func (d *Dialer) Done() <-chan struct{} { return d.done }

// Err returns the reason the Dialer is done: the fatal session error that
// tore it down, ErrSessionClosed after a deliberate Close, or nil while the
// Dialer is still usable.
func (d *Dialer) Err() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fatalErr != nil {
		return d.fatalErr
	}
	if d.ctx.Err() != nil {
		return ErrSessionClosed
	}
	return nil
}

// DialContext opens a new stream targeting addr (of the form 'host:port').
// The port _must_ be numeric, and the host must refer to a private endpoint
// within this Dialer's associated account. For example, if this account has
// an endpoint created with 'ngrok http --url "http://foo.internal" 8080', the
// expected invocation to reach that endpoint would be:
//
//	conn, err := dialer.DialContext(ctx, "tcp", "foo.internal:80")
//
// The returned net.Conn is a bidirectional byte stream.
//
// The server commits to the stream before it knows the dial outcome, so a
// server-side dial failure is reported via HTTP trailers and surfaces on the
// first Read of the returned conn (in place of io.EOF) rather than from
// DialContext itself. That error is a *net.OpError wrapping a
// privatedial.Error, and — for error codes with a net analogue — also wrapping
// a standard sentinel so callers can branch with errors.Is/As as before:
//   - an unauthorized or "session draining" rejection wraps
//     syscall.ECONNREFUSED — analogous to a refused TCP connect.
//   - other codes carry no sentinel; recover the ngrok code with
//     errors.As(err, &nerr) where nerr is a privatedial.Error.
//
// On a Dialer that has not connected yet, DialContext first calls Connect;
// an error establishing that initial connection is returned as a
// *net.OpError wrapping the Connect error.
//
// Dialing on a Dialer that has been closed, or torn down by a fatal
// session error, fails with a *net.OpError wrapping ErrSessionClosed.
func (d *Dialer) DialContext(ctx context.Context, network string, addr string) (net.Conn, error) {
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
	if err := d.ensureConnected(ctx); err != nil {
		return nil, &net.OpError{Op: "dial", Net: network, Err: err}
	}

	// we validated 'addr' is well formed, so we can just return it up to callers
	// and save having to format in the port each time.
	return d.dial(ctx, dialAddr{
		addr: addr,
		host: host,
		port: int(port),
	})
}

func (d *Dialer) dial(ctx context.Context, addr dialAddr) (net.Conn, error) {
	// budgetCtx bounds the entire dial: both the wait for a usable conn and the
	// per-conn RoundTrip. The cause lets us map our own budget expiry to a
	// refused connect without treating caller cancellation the same way.
	budgetCtx, cancel := context.WithTimeoutCause(ctx, d.dialWaitTimeout(), errDialWaitTimeout)
	defer cancel()

	dialBudgetExpired := func() bool {
		return errors.Is(context.Cause(budgetCtx), errDialWaitTimeout)
	}

	dialErrFor := func(err error) error {
		if ctx.Err() == nil && (dialBudgetExpired() || errors.Is(err, context.DeadlineExceeded)) {
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
		if err := ctx.Err(); err != nil {
			return nil, dialErrFor(err)
		}
		if dialBudgetExpired() {
			return nil, dialErrFor(context.DeadlineExceeded)
		}
		cur, err := d.waitForCurrent(budgetCtx)
		if err != nil {
			return nil, dialErrFor(err)
		}
		conn, dialErr := cur.dial(budgetCtx, addr)
		if dialErr == nil {
			return conn, nil
		}
		if dialBudgetExpired() {
			return nil, dialErrFor(context.DeadlineExceeded)
		}
		if errors.Is(dialErr, context.Canceled) || errors.Is(dialErr, context.DeadlineExceeded) {
			return nil, dialErr
		}
		var stale *staleConnError
		if !errors.As(dialErr, &stale) {
			return nil, dialErr
		}
		d.log.Debug("dial raced a stale conn, retrying on a fresh one", "addr", addr.addr, "error", dialErr)
	}
}

func (d *Dialer) waitForCurrent(ctx context.Context) (*sessionConn, error) {
	for {
		if err := d.ctx.Err(); err != nil {
			return nil, ErrSessionClosed
		}
		cur, ready, fatal := d.snapshot()
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
		case <-d.ctx.Done():
			return nil, ErrSessionClosed
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

func (d *Dialer) dialWaitTimeout() time.Duration {
	if d.dialWait > 0 {
		return d.dialWait
	}
	return defaultDialWait
}

func (d *Dialer) snapshot() (*sessionConn, <-chan struct{}, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.current, d.ready, d.fatalErr
}

func (d *Dialer) supervise() {
	for {
		d.mu.Lock()
		cur := d.current
		d.mu.Unlock()
		if cur == nil {
			next, err := d.reconnect()
			if err != nil {
				return
			}
			d.swapCurrent(next)
			continue
		}
		select {
		case <-d.ctx.Done():
			return
		case <-cur.drainCh:
			d.parkDraining(cur, cur.drainGrace)
		case err := <-cur.serverErrCh:
			select {
			case <-cur.drainCh:
				d.parkDraining(cur, cur.drainGrace)
			default:
				cur.log.Debug("control stream failed, reconnecting", "error", err)
				d.removeCurrent(cur)
			}
		}
	}
}

func (d *Dialer) reconnect() (*sessionConn, error) {
	boff := newReconnectBackoff(reconnectBackoffMinDelay, reconnectBackoffMaxDelay, d.sessionClock())
	for {
		if err := d.ctx.Err(); err != nil {
			return nil, err
		}
		attemptCtx, cancel := context.WithTimeout(d.ctx, dialTimeout)
		h, err := d.openFn(attemptCtx)
		cancel()
		if err == nil {
			if cerr := d.ctx.Err(); cerr != nil {
				_ = h.close()
				return nil, cerr
			}
			h.log.Debug("reconnected")
			return h, nil
		}
		var fatal *authFatalError
		if errors.As(err, &fatal) {
			d.log.Debug("reconnect failed with fatal auth error, giving up", "error", err)
			d.setFatal(err)
			return nil, err
		}
		d.log.Debug("reconnect attempt failed, backing off", "error", err, "backoff", boff.next)
		if err := boff.Wait(d.ctx); err != nil {
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

func (d *Dialer) sessionClock() sessionClock {
	if d.clock != nil {
		return d.clock
	}
	return realClock{}
}

func (d *Dialer) swapCurrent(h *sessionConn) {
	d.mu.Lock()
	if d.ctx.Err() != nil {
		d.mu.Unlock()
		_ = h.close()
		return
	}
	d.current = h
	d.signalReadyLocked()
	d.mu.Unlock()
}

func (d *Dialer) parkDraining(old *sessionConn, grace time.Duration) {
	closeNow := false
	d.mu.Lock()
	if d.ctx.Err() != nil {
		closeNow = true
	} else {
		if d.current == old {
			d.current = nil
		}
		d.signalReadyLocked()

		if grace <= 0 {
			closeNow = true
		} else {
			old.log.Debug("server requested drain, parking connection", "grace", grace)
			clk := d.sessionClock()
			d.drainGroup.Add(1)
			go func() {
				defer d.drainGroup.Done()
				timer := clk.NewTimer(grace)
				defer timer.Stop()
				select {
				case <-timer.C():
				case <-d.ctx.Done():
				}
				old.log.Debug("drain grace period over, closing parked connection")
				_ = old.close()
			}()
		}
	}
	d.mu.Unlock()
	if closeNow {
		_ = old.close()
	}
}

func (d *Dialer) removeCurrent(cur *sessionConn) {
	d.mu.Lock()
	if d.current == cur {
		d.current = nil
		d.signalReadyLocked()
	}
	d.mu.Unlock()
	_ = cur.close()
}

func (d *Dialer) setFatal(err error) {
	d.mu.Lock()
	if d.fatalErr == nil {
		d.fatalErr = err
		d.doneOnce.Do(func() { close(d.done) })
	}
	d.signalReadyLocked()
	d.mu.Unlock()
}

func (d *Dialer) signalReadyLocked() {
	close(d.ready)
	d.ready = make(chan struct{})
}

// Close tears down the Dialer: the supervisor, the active conn, and any conns
// still in their drain grace window. In-flight Dial streams will see
// read/write errors, and subsequent Connect or DialContext calls fail with
// ErrSessionClosed.
func (d *Dialer) Close() error {
	d.closeOnce.Do(func() {
		d.log.Debug("closing dialer")
		d.cancel()
		d.doneOnce.Do(func() { close(d.done) })
		d.mu.Lock()
		cur := d.current
		d.current = nil
		d.signalReadyLocked()
		d.mu.Unlock()
		if cur != nil {
			_ = cur.close()
		}
		d.drainGroup.Wait()
	})
	return nil
}

type sessionConn struct {
	log          *slog.Logger
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

	controlResp     *http.Response
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

	closeOnce sync.Once
}

func (h *sessionConn) dial(ctx context.Context, addr dialAddr) (net.Conn, error) {
	h.log.Debug("dialing endpoint", "addr", addr.addr)
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
		return nil, &net.OpError{
			Op:   "dial",
			Net:  addr.Network(),
			Addr: addr,
			Err:  fmt.Errorf("private-dial /dial status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet))),
		}
	}

	return &dialConn{
		remoteAddr: addr,
		reqBody:    reqWriter,
		resp:       resp,
		respBody:   resp.Body,
		reqCancel:  reqCancel,
	}, nil
}

// The HTTP trailer names a private-dial server uses to report a structured
// ngrok error when a /dial or /session stream terminates. Because they are
// trailers, they only become readable once the response body reaches EOF.
const (
	dialErrorCodeTrailer    = "Ngrok-Dial-Error-Code"
	dialErrorMessageTrailer = "Ngrok-Dial-Error-Message"
)

// Error is the structured error a private-dial server reports via the
// dial/session HTTP trailers. It carries the ngrok error code so callers can
// branch on it; all ngrok error codes are documented at
// https://ngrok.com/docs/errors.
//
// Extract it from any error returned by this package with errors.As:
//
//	var nerr privatedial.Error
//	if errors.As(err, &nerr) {
//		fmt.Printf("%s: %s\n", nerr.Code(), nerr)
//	}
type Error interface {
	error
	// Code returns the prefixed ngrok error code (e.g. "ERR_NGROK_706").
	// It is empty if the server did not send one.
	Code() string
}

// ErrSessionClosed is reported by Connect and DialContext when the Dialer has
// been torn down, either by Close or by a fatal session error. From
// DialContext it arrives wrapped in a *net.OpError; match it with
// errors.Is(err, ErrSessionClosed).
var ErrSessionClosed = errors.New("private-dial: session closed")

const ngrokErrorsURL = "https://ngrok.com/docs/errors"

// serverError is the concrete Error rehydrated from the dial/session trailers.
// It optionally wraps a net-style sentinel (chosen from its ngrok error code by
// netSentinelForCode) so callers who only care about the broad failure category
// can match it with errors.Is/errors.As against the standard net errors — e.g.
// errors.As(&net.DNSError{}) or errors.Is(err, syscall.ECONNREFUSED) — while
// errors.As(&privatedial.Error) still recovers the ngrok code.
type serverError struct {
	code    string
	message string
	// wrapped is the net-style sentinel (*net.DNSError,
	// *os.SyscallError{ECONNREFUSED}, ...) this error code maps to, or nil
	// when the code has no net analogue. It is purely a type/Is match anchor;
	// its own text is not part of Error().
	wrapped error
}

func (e *serverError) Code() string { return e.code }

func (e *serverError) Error() string {
	msg := e.message
	if e.code != "" {
		if msg == "" {
			msg = e.code
		}
		return fmt.Sprintf("%s\n\n%s/%s", msg, ngrokErrorsURL, strings.ToLower(e.code))
	}
	return msg
}

// Unwrap returns the net-style sentinel chosen for this error's ngrok code, so
// errors.Is/errors.As walks down to it.
func (e *serverError) Unwrap() error { return e.wrapped }

// Private-dial ngrok error codes that carry a net-level analogue. The server
// reports exactly these two on a dial/session trailer, and both behave like a
// refused connect from the caller's POV — mirroring the old status-based
// mapping (an unauthorized 401/403 or a "session draining" 503 -> ECONNREFUSED).
const (
	errCodeUnauthorized    = "ERR_NGROK_709"
	errCodeSessionDraining = "ERR_NGROK_742"
)

// netSentinelForCode maps a private-dial ngrok error code to the net-style
// sentinel that best matches it. The two codes with a net analogue both behave
// like a refused connect; any other code has no sentinel, leaving the failure
// to surface only as an Error.
func netSentinelForCode(code string) error {
	switch code {
	case errCodeUnauthorized, errCodeSessionDraining:
		return &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}
	default:
		return nil
	}
}

// errorFromTrailer rehydrates the structured ngrok error a private-dial server
// reported via the HTTP trailers of a /dial or /session response. It returns
// nil when the trailers carry no error, so a clean stream termination is
// distinguishable from a server-reported failure. The trailers are only
// populated once the corresponding response body has been read to EOF.
func errorFromTrailer(trailer http.Header) Error {
	code := trailer.Get(dialErrorCodeTrailer)
	message := strings.TrimSpace(trailer.Get(dialErrorMessageTrailer))
	if code == "" && message == "" {
		return nil
	}
	return &serverError{code: code, message: message, wrapped: netSentinelForCode(code)}
}

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
		h.log.Debug("closing session conn")
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

// errConnClosed is returned by sendControlFrame when the conn has been
// closed or the controlSender goroutine has exited.
var errConnClosed = errors.New("private-dial: conn closed")

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
		return errConnClosed
	case <-h.sendDone:
		return errConnClosed
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

// pingLoop sends a Ping every pingInterval and resolves the token when the
// matching Pong arrives. It stops on Close.
//
// Each tick uses a per-send timeout of pingInterval so a stuck sender
// can't pin successive ticks; transient timeouts just skip a ping and
// let the next tick try again. Terminal errConnClosed exits the loop.
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
			if errors.Is(err, errConnClosed) {
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
// server can measure its RTT to us), resolves outstanding pings on Pong,
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
				// The control stream ended. Its trailers are now readable;
				// a server-reported error preempts the plain io.EOF the
				// deferred send would otherwise forward.
				if h.controlResp != nil {
					if rerr := errorFromTrailer(h.controlResp.Trailer); rerr != nil {
						h.log.Debug("control stream ended with server-reported error", "error", rerr)
						select {
						case h.serverErrCh <- rerr:
						default:
						}
						return
					}
				}
				h.log.Debug("control stream ended")
				return
			}
			h.log.Debug("control stream read failed", "error", err)
			select {
			case h.serverErrCh <- err:
			default:
			}
			return
		}
		switch f := frame.Frame.(type) {
		case *pbpd.ControlFrame_PleaseDrain:
			grace := time.Duration(f.PleaseDrain.GetGracePeriodSeconds()) * time.Second
			h.log.Debug("received PleaseDrain", "grace", grace)
			h.markDraining(grace)
		case *pbpd.ControlFrame_SessionError:
			h.log.Debug("received SessionError", "message", f.SessionError.GetMessage())
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
			// completePing keeps the outstanding-ping map from growing; the
			// measured RTT is not otherwise surfaced anywhere.
			if rtt, ok := h.completePing(f.Pong.GetToken(), time.Now()); ok {
				h.log.Debug("ping round-trip completed", "rtt", rtt)
			}
		}
	}
}

// dialConn is the net.Conn returned from Dialer.DialContext. Read pulls
// from the response body; Write pushes into the request body. Close terminates
// both sides.
//
// Because the server commits to a 200 before it knows whether the dial will
// succeed, dial failures are reported via HTTP trailers that only become
// readable once the response body reaches EOF. Read therefore inspects the
// trailers on EOF: a server-reported failure surfaces as a privatedial.Error
// in place of io.EOF, while a clean stream termination surfaces as io.EOF.
type dialConn struct {
	reqBody   *io.PipeWriter
	resp      *http.Response
	respBody  io.ReadCloser
	reqCancel context.CancelFunc

	remoteAddr dialAddr

	closeOnce sync.Once
}

func (c *dialConn) Read(p []byte) (int, error) {
	n, err := c.respBody.Read(p)
	if errors.Is(err, io.EOF) {
		if rerr := errorFromTrailer(c.resp.Trailer); rerr != nil {
			// Wrap as *net.OpError so the failure category (the net
			// sentinel netSentinelForCode chose) bubbles via errors.Is /
			// errors.As just as the old status-based dial error did.
			return n, &net.OpError{Op: "dial", Net: c.remoteAddr.Network(), Addr: c.remoteAddr, Err: rerr}
		}
	}
	return n, err
}

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
