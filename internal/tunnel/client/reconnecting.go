package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jpillora/backoff"

	"golang.ngrok.com/ngrok/v2/internal/tunnel/netx"
	"golang.ngrok.com/ngrok/v2/internal/tunnel/proto"
)

var ErrSessionNotReady = errors.New("an ngrok tunnel session has not yet been established")

// Wraps a RawSession so that it can be safely swapped out
type swapRaw struct {
	raw atomic.Pointer[RawSession]
	// live trails raw: the connection exists from set() but cannot carry
	// binds until Auth and the rebinds have gone through on it.
	live atomic.Bool
}

func (s *swapRaw) get() RawSession {
	ptr := s.raw.Load()
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (s *swapRaw) set(raw RawSession) {
	s.raw.Store(&raw)
}

// clear marks this leg as not connected. Without it ready would mean "has
// connected at least once", and a leg that dropped and is still backing off
// would be handed out as if it were live.
func (s *swapRaw) clear() {
	s.live.Store(false)
	s.raw.Store(nil)
}

// serving marks the leg usable for new binds. Auth itself needs the raw
// session set, so a leg mid-handshake would otherwise be picked by readyLeg
// and the bind would go out before the server authed it.
func (s *swapRaw) serving() {
	s.live.Store(true)
}

// ready reports whether this leg currently has a live connection. Legs dial
// independently, so callers must not assume any particular leg is up.
func (s *swapRaw) ready() bool {
	return s.live.Load()
}

func (s *swapRaw) Auth(id string, extra proto.AuthExtra) (resp proto.AuthResp, err error) {
	if raw := s.get(); raw != nil {
		return raw.Auth(id, extra)
	}
	return proto.AuthResp{}, ErrSessionNotReady
}

func (s *swapRaw) Listen(protocol string, opts any, extra proto.BindExtra, id string, forwardsTo string, forwardsProto string) (resp proto.BindResp, err error) {
	if raw := s.get(); raw != nil {
		return raw.Listen(protocol, opts, extra, id, forwardsTo, forwardsProto)
	}
	return proto.BindResp{}, ErrSessionNotReady
}

func (s *swapRaw) ListenLabel(labels map[string]string, metadata string, forwardsTo string, forwardsProto string) (resp proto.StartTunnelWithLabelResp, err error) {
	if raw := s.get(); raw != nil {
		return raw.ListenLabel(labels, metadata, forwardsTo, forwardsProto)
	}
	return proto.StartTunnelWithLabelResp{}, ErrSessionNotReady
}

func (s *swapRaw) Unlisten(url string) (resp proto.UnbindResp, err error) {
	if raw := s.get(); raw != nil {
		return raw.Unlisten(url)
	}
	return proto.UnbindResp{}, ErrSessionNotReady
}

func (s *swapRaw) PatchTunnelState(tunnelID string, name, description, metadata *string, poolingEnabled *bool, trafficPolicy *string) (resp proto.PatchTunnelStateResp, err error) {
	if raw := s.get(); raw != nil {
		return raw.PatchTunnelState(tunnelID, name, description, metadata, poolingEnabled, trafficPolicy)
	}
	return proto.PatchTunnelStateResp{}, ErrSessionNotReady
}

func (s *swapRaw) SrvInfo() (resp proto.SrvInfoResp, err error) {
	if raw := s.get(); raw != nil {
		return raw.SrvInfo()
	}
	return proto.SrvInfoResp{}, ErrSessionNotReady
}

func (s *swapRaw) SendAgentMetrics(ctx context.Context, batch []byte) ([]byte, error) {
	if raw := s.get(); raw != nil {
		return raw.SendAgentMetrics(ctx, batch)
	}
	return nil, ErrSessionNotReady
}

func (s *swapRaw) Heartbeat() (time.Duration, error) {
	if raw := s.get(); raw != nil {
		return raw.Heartbeat()
	}
	return 0, ErrSessionNotReady
}

func (s *swapRaw) Latency() <-chan time.Duration {
	if raw := s.get(); raw != nil {
		return raw.Latency()
	}
	return nil
}

func (s *swapRaw) Close() error {
	raw := s.get()
	if raw == nil {
		return nil
	}
	return raw.Close()
}

func (s *swapRaw) Accept() (netx.LoggedConn, error) {
	return s.get().Accept()
}

type reconnectingSession struct {
	closed       int32
	dialer       RawSessionDialer
	stateChanges chan<- error
	cb           ReconnectCallback
	// sessionsMu guards sessions. Legs are appended by whichever leg goroutine
	// first learns the leg count, and read by callers binding tunnels.
	sessionsMu sync.RWMutex
	sessions   []*leg
	// tunnelsMu guards tunnels, the endpoints the caller has open. Every leg
	// binds this whole set, so a leg that was down while one was created
	// picks it up when it reconnects. Keyed by pointer because a label
	// tunnel's id is assigned per bind and differs between legs.
	tunnelsMu sync.RWMutex
	tunnels   map[*tunnel]struct{}
	// readyMu owns readyCount, which decides whether a leg's state change is
	// the whole session's. The consumer sees one established signal for the
	// session rather than one per leg, and a region the client cannot reach
	// stays quiet while another leg serves.
	readyMu    sync.Mutex
	readyCount int
	// clientIDMu guards clientID, which every leg's Auth reads and writes.
	clientIDMu        sync.Mutex
	clientID          string
	failPermanentOnce sync.Once
	*slog.Logger
}

// leg is one underlying connection of a reconnecting session. Each leg dials
// its own region and binds the open endpoints independently of the others.
type leg struct {
	*session
	swapper *swapRaw
	number  uint32
	// bindMu serializes changes to what this leg has bound, so a mirrored
	// bind cannot be lost by a concurrent rebind of the whole open set. The
	// proxy path never takes it, so an in-flight bind never blocks a
	// connection lookup.
	bindMu sync.Mutex
}

func newLeg(s *reconnectingSession, logger *slog.Logger, number uint32) *leg {
	swapper := new(swapRaw)
	l := &leg{
		session: &session{
			raw:     swapper,
			Logger:  newLogger(logger),
			tunnels: make(map[string]*tunnel),
		},
		swapper: swapper,
		number:  number,
	}
	l.session.unbind = s.unlistenTunnel
	return l
}

type (
	RawSessionDialer  func(legNumber uint32) (RawSession, error)
	ReconnectCallback func(s Session, r RawSession, legNumber uint32) (int, error)
)

// Establish Session(s) that reconnect across temporary network failures. The
// returned Session object uses the given dialer to reconnect whenever Accept
// would have failed with a temporary error. When a reconnecting session is
// re-established, it reissues the Auth call and Listen calls for each tunnel
// that it previously had open.
//
// Whenever the Session suffers a temporary failure, it publishes the error
// encountered over the provided stateChanges channel. If a connection is
// established, it publishes nil over that channel. If the Session suffers
// a permanent failure, the stateChanges channel is closed.
//
// It is unsafe to call any functions except Close() on the returned session until
// you receive the first callback.
//
// If the stateChanges channel is not serviced by the caller, the
// ReconnectingSession will hang.
//
// When using MultiLeg, each leg binds the open endpoints independently. A new
// bind lands on the first ready leg and is mirrored to the rest best effort.
func NewReconnectingSession(logger *slog.Logger, dialer RawSessionDialer, stateChanges chan<- error, cb ReconnectCallback) Session {
	s := &reconnectingSession{
		dialer:       dialer,
		stateChanges: stateChanges,
		cb:           cb,
		tunnels:      make(map[*tunnel]struct{}),
		Logger:       logger,
	}

	// setup an initial connection
	s.ensureLegs(1)

	return s
}

// ensureLegs brings the session up to desired legs. The shortfall is decided
// and filled under one lock, so two legs learning the count at once cannot
// both append from the same snapshot.
func (s *reconnectingSession) ensureLegs(desired int) {
	s.sessionsMu.Lock()
	var fresh []*leg
	for len(s.sessions) < desired {
		l := newLeg(s, s.Logger, uint32(len(s.sessions)))
		s.sessions = append(s.sessions, l)
		fresh = append(fresh, l)
	}
	s.sessionsMu.Unlock()

	for _, l := range fresh {
		go func(l *leg) {
			if err := s.connect(nil, l); err != nil {
				return
			}
			s.receive(l)
		}(l)
	}
}

// legs returns a snapshot so callers never hold sessionsMu across a network
// call to a leg.
func (s *reconnectingSession) legs() []*leg {
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()
	return append([]*leg(nil), s.sessions...)
}

// readyLeg returns the lowest-numbered leg that is currently connected. An
// unreachable region must not deny service to the legs that did connect, so
// no specific leg is required.
func (s *reconnectingSession) readyLeg() *leg {
	for _, l := range s.legs() {
		if l.swapper.ready() {
			return l
		}
	}
	return nil
}

// legServing marks a leg usable for new binds. It reports whether the session
// went from having no usable leg to having one, which is what the caller
// publishes as established. The send stays out of readyMu, because a consumer
// that stops reading stateChanges would otherwise freeze every other leg.
func (s *reconnectingSession) legServing(l *leg) bool {
	s.readyMu.Lock()
	defer s.readyMu.Unlock()
	if l.swapper.ready() {
		return false
	}
	l.swapper.serving()
	s.readyCount++
	return s.readyCount == 1
}

// legDown marks a leg unusable and reports whether the session has no leg
// left, which is the only point at which a failure is the whole session's.
func (s *reconnectingSession) legDown(l *leg) bool {
	s.readyMu.Lock()
	defer s.readyMu.Unlock()
	if l.swapper.ready() {
		l.swapper.clear()
		s.readyCount--
	}
	return s.readyCount == 0
}

func (s *reconnectingSession) Heartbeat() (time.Duration, error) {
	if l := s.readyLeg(); l != nil {
		return l.Heartbeat()
	}
	return 0, ErrSessionNotReady
}

func (s *reconnectingSession) Latency() <-chan time.Duration {
	if l := s.readyLeg(); l != nil {
		return l.Latency()
	}
	return nil
}

func (s *reconnectingSession) Listen(protocol string, opts any, extra proto.BindExtra, forwardsTo string, forwardsProto string) (Tunnel, error) {
	return s.listenTunnel(func(session *session) (Tunnel, error) {
		return session.Listen(protocol, opts, extra, forwardsTo, forwardsProto)
	})
}

func (s *reconnectingSession) ListenLabel(labels map[string]string, metadata string, forwardsTo string, forwardsProto string) (Tunnel, error) {
	return s.listenTunnel(func(session *session) (Tunnel, error) {
		return session.ListenLabel(labels, metadata, forwardsTo, forwardsProto)
	})
}

func (s *reconnectingSession) listenTunnel(listen func(*session) (Tunnel, error)) (Tunnel, error) {
	primary := s.readyLeg()
	if primary == nil {
		return nil, ErrSessionNotReady
	}

	t, err := listen(primary.session)
	if err != nil {
		return nil, err
	}
	tun := t.(*tunnel)

	s.tunnelsMu.Lock()
	s.tunnels[tun] = struct{}{}
	s.tunnelsMu.Unlock()

	for _, l := range s.legs() {
		if l == primary {
			continue
		}
		s.bindTunnelToLeg(l, tun)
	}
	return tun, nil
}

// openTunnels snapshots the endpoints every leg is expected to be serving.
func (s *reconnectingSession) openTunnels() []*tunnel {
	s.tunnelsMu.RLock()
	defer s.tunnelsMu.RUnlock()
	open := make([]*tunnel, 0, len(s.tunnels))
	for t := range s.tunnels {
		open = append(open, t)
	}
	return open
}

// bindTunnelToLeg mirrors an already-bound tunnel onto another leg. Failure is
// logged and dropped, because the endpoint is already serving elsewhere and
// tearing it down because a second region is unreachable would turn a
// degraded session into an outage. The leg rebinds it on its next reconnect.
func (s *reconnectingSession) bindTunnelToLeg(l *leg, tun *tunnel) {
	l.bindMu.Lock()
	defer l.bindMu.Unlock()

	// a leg that is not ready yet picks this tunnel up from the open set when
	// it rebinds, which is also serialized by bindMu
	if !l.swapper.ready() {
		return
	}
	id, err := s.bindOnLeg(l.raw, tun, tun.ID())
	if err != nil {
		s.Error("failed to bind tunnel on leg, continuing without it",
			"leg", l.number, "id", tun.ID(), "err", err)
		return
	}
	l.addTunnel(id, tun)
}

// unlistenTunnel unbinds a tunnel everywhere it is bound. Each leg assigns its
// own id, so the id to unbind is the one in that leg's map.
func (s *reconnectingSession) unlistenTunnel(tun *tunnel) error {
	s.tunnelsMu.Lock()
	delete(s.tunnels, tun)
	s.tunnelsMu.Unlock()

	var errs []error
	for _, l := range s.legs() {
		id, err := l.unbindTunnel(tun)
		if err != nil {
			errs = append(errs, fmt.Errorf("leg %d unbinding %s: %w", l.number, id, err))
		}
	}
	return errors.Join(errs...)
}

// unbindTunnel releases tun from this leg if it holds it. A leg that is down
// has nothing bound remotely, so it reports no failure.
func (l *leg) unbindTunnel(tun *tunnel) (string, error) {
	l.bindMu.Lock()
	defer l.bindMu.Unlock()

	if !l.swapper.ready() {
		return "", nil
	}
	id, ok := l.idFor(tun)
	if !ok {
		return "", nil
	}
	return id, l.unlisten(id)
}

func (s *reconnectingSession) PatchTunnelState(tunnelID string, name, description, metadata *string, poolingEnabled *bool, trafficPolicy *string) error {
	if l := s.readyLeg(); l != nil {
		return l.PatchTunnelState(tunnelID, name, description, metadata, poolingEnabled, trafficPolicy)
	}
	return ErrSessionNotReady
}

func (s *reconnectingSession) SrvInfo() (resp proto.SrvInfoResp, err error) {
	if l := s.readyLeg(); l != nil {
		return l.SrvInfo()
	}
	return proto.SrvInfoResp{}, ErrSessionNotReady
}

func (s *reconnectingSession) ListenHTTP(opts *proto.HTTPEndpoint, extra proto.BindExtra, forwardsTo string, forwardsProto string) (Tunnel, error) {
	return s.Listen("http", opts, extra, forwardsTo, forwardsProto)
}

func (s *reconnectingSession) ListenHTTPS(opts *proto.HTTPEndpoint, extra proto.BindExtra, forwardsTo string, forwardsProto string) (Tunnel, error) {
	return s.Listen("https", opts, extra, forwardsTo, forwardsProto)
}

func (s *reconnectingSession) ListenTCP(opts *proto.TCPEndpoint, extra proto.BindExtra, forwardsTo string) (Tunnel, error) {
	return s.Listen("tcp", opts, extra, forwardsTo, "")
}

func (s *reconnectingSession) ListenTLS(opts *proto.TLSEndpoint, extra proto.BindExtra, forwardsTo string) (Tunnel, error) {
	return s.Listen("tls", opts, extra, forwardsTo, "")
}

func (s *reconnectingSession) Close() error {
	atomic.StoreInt32(&s.closed, 1)
	var errs []error
	for _, l := range s.legs() {
		if err := l.Close(); err != nil {
			errs = append(errs, fmt.Errorf("leg %d closing: %w", l.number, err))
		}
	}
	return errors.Join(errs...)
}

// CloseTunnel closes a tunnel the server has rejected. The id is the one the
// rejecting leg bound it under, and every other leg may hold it under its
// own, so the lookup stops at the leg that knows it.
func (s *reconnectingSession) CloseTunnel(clientID string, e error) error {
	for _, l := range s.legs() {
		t, ok := l.getTunnel(clientID)
		if !ok {
			continue
		}
		// the remote already dropped it, so it must not come back on the
		// next leg to reconnect
		s.tunnelsMu.Lock()
		delete(s.tunnels, t)
		s.tunnelsMu.Unlock()

		t.CloseWithError(e)
		return nil
	}
	return proto.StringError("no listener found for client id " + clientID)
}

func (s *reconnectingSession) receive(l *leg) {
	// when we shut down, close all of the open tunnels
	defer func() {
		l.RLock()
		for _, t := range l.tunnels {
			go t.Close()
		}
		l.RUnlock()
	}()

	for {
		// accept the next proxy connection
		proxy, err := l.raw.Accept()
		if err == nil {
			go l.handleProxy(proxy)
			continue
		}

		// we disconnected, reconnect
		err = s.connect(err, l)
		if err != nil {
			l.Info("accept failed", "err", err)
			// permanent failure
			return
		}
	}
}

// restartBinds rebinds the whole open set on one leg and then marks it
// serving. A leg that was down while an endpoint was created catches up here
// rather than staying behind it until some later full reconnect. The remote
// calls run without the leg's own lock so the proxy path keeps reading the
// map it is still serving from. The returned flag reports that this leg is
// the one that brought the session up.
func (s *reconnectingSession) restartBinds(l *leg) (established bool, err error) {
	l.bindMu.Lock()
	defer l.bindMu.Unlock()

	open := s.openTunnels()
	raw := l.raw

	// reconnected tunnels, which may have different IDs
	newTunnels := make(map[string]*tunnel, len(open))
	for _, t := range open {
		oldID := t.ID()
		if id, ok := l.idFor(t); ok {
			oldID = id
		}
		id, err := s.bindOnLeg(raw, t, oldID)
		if err != nil {
			return false, err
		}
		newTunnels[id] = t
	}

	l.Lock()
	l.tunnels = newTunnels
	l.Unlock()

	return s.legServing(l), nil
}

func (s *reconnectingSession) Auth(extra proto.AuthExtra) (resp proto.AuthResp, err error) {
	legs := s.legs()
	if len(legs) < int(extra.LegNumber) {
		err = errors.New("leg number out of range")
		return
	}
	s.clientIDMu.Lock()
	clientID := s.clientID
	s.clientIDMu.Unlock()

	resp, err = legs[extra.LegNumber].raw.Auth(clientID, extra)
	if err != nil {
		return
	}
	if resp.Error != "" {
		err = proto.StringError(resp.Error)
		return
	}
	s.clientIDMu.Lock()
	s.clientID = resp.ClientID
	s.clientIDMu.Unlock()
	return
}

func (s *reconnectingSession) connect(acceptErr error, connLeg *leg) error {
	boff := &backoff.Backoff{
		Min:    500 * time.Millisecond,
		Max:    30 * time.Second,
		Factor: 2,
		Jitter: false,
	}

	failTemp := func(err error, raw RawSession) {
		// the failure reaches the caller only when no leg is left, so log it
		// here or a degraded session loses it entirely
		s.Error("failed to reconnect session", "leg", connLeg.number, "err", err)

		// the leg is down for as long as we are backing off, so it must not be
		// picked to bind on
		if s.legDown(connLeg) {
			s.stateChanges <- err
		}

		// if the retry loop failed after the session was opened, then make sure to close it
		if raw != nil {
			raw.Close()
		}

		// session failed, wait before reconnecting
		wait := boff.Duration()
		s.Debug("sleep before reconnect", "secs", int(wait.Seconds()))
		time.Sleep(wait)
	}

	failPermanent := func(err error) error {
		s.failPermanentOnce.Do(func() {
			s.stateChanges <- err
			close(s.stateChanges)
		})
		return err
	}

	if acceptErr != nil {
		// the connection behind this leg is gone, so stop advertising it as
		// usable until the dial below succeeds
		last := s.legDown(connLeg)
		if last && atomic.LoadInt32(&s.closed) == 0 {
			connLeg.Error("session closed, starting reconnect loop", "err", acceptErr)
			s.stateChanges <- acceptErr
		}
	}

	for {
		// don't try to reconnect if the session was closed explicitly
		// by the client side
		if atomic.LoadInt32(&s.closed) == 1 {
			// intentionally ignoring error
			_ = failPermanent(errors.New("not reconnecting, session closed by the client side"))
			return errors.New("reconnecting session closed")
		}

		// dial the tunnel server
		raw, err := s.dialer(connLeg.number)
		if err != nil {
			failTemp(err, raw)
			continue
		}

		// successfully reconnected
		connLeg.swapper.set(raw)

		// callback for authentication
		desiredLegs, err := s.cb(s, raw, connLeg.number)
		if err != nil {
			failTemp(err, raw)
			continue
		}

		// spawn every remaining leg at once. Chaining them meant an
		// unreachable region stalled the legs queued behind it.
		s.ensureLegs(desiredLegs)

		// re-establish binds, which also marks this leg serving
		established, err := s.restartBinds(connLeg)
		if err != nil {
			failTemp(err, raw)
			continue
		}

		// reset wait
		boff.Reset()

		s.Info("client session established", "leg", connLeg.number)
		if established {
			s.stateChanges <- nil
		}
		return nil
	}
}

// bindOnLeg binds t on one leg and reports the id that leg bound it under,
// which is not the id any other leg holds it under.
func (s *reconnectingSession) bindOnLeg(raw RawSession, t *tunnel, oldID string) (string, error) {
	// set the returned token for reconnection
	tCfg := t.RemoteBindConfig()
	t.bindExtra.Token = tCfg.Token

	id := oldID
	var respErr string
	if tCfg.Labels != nil {
		resp, err := raw.ListenLabel(tCfg.Labels, tCfg.Metadata, t.ForwardsTo(), t.ForwardsProto())
		if err != nil {
			return "", err
		}
		respErr = resp.Error
		if resp.ID != "" {
			t.id.Store(resp.ID)
			id = resp.ID
		}
	} else {
		resp, err := raw.Listen(tCfg.ConfigProto, tCfg.Opts, t.bindExtra, t.ID(), t.ForwardsTo(), t.ForwardsProto())
		if err != nil {
			return "", err
		}
		respErr = resp.Error
	}

	if respErr != "" {
		return "", errors.New(respErr)
	}
	return id, nil
}
