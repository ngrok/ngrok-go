package client

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/inconshreveable/log15/v3"
	"github.com/jpillora/backoff"

	"golang.ngrok.com/ngrok/internal/tunnel/netx"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

var ErrSessionNotReady = errors.New("an ngrok tunnel session has not yet been established")

// Wraps a RawSession so that it can be safely swapped out
type swapRaw struct {
	raw atomic.Pointer[RawSession]
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
	s.raw.Store(nil)
}

// ready reports whether this leg currently has a live connection. Legs dial
// independently, so callers must not assume any particular leg is up.
func (s *swapRaw) ready() bool {
	return s.get() != nil
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

func (s *swapRaw) SrvInfo() (resp proto.SrvInfoResp, err error) {
	if raw := s.get(); raw != nil {
		return raw.SrvInfo()
	}
	return proto.SrvInfoResp{}, ErrSessionNotReady
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
	clientID     string
	cb           ReconnectCallback
	// sessionsMu guards sessions. Legs are appended by whichever leg goroutine
	// first learns the leg count, and read by callers binding tunnels.
	sessionsMu        sync.RWMutex
	sessions          []*session
	failPermanentOnce sync.Once
	log.Logger
}

type RawSessionDialer func(legNumber uint32) (RawSession, error)
type ReconnectCallback func(s Session, r RawSession, legNumber uint32) (int, error)

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
// When using MultiLeg, there will be multiple underlying Sessions which are kept
// in sync. This struct will broadcast calls to all underlying Sessions.
func NewReconnectingSession(logger log.Logger, dialer RawSessionDialer, stateChanges chan<- error, cb ReconnectCallback) Session {
	s := &reconnectingSession{
		dialer:       dialer,
		stateChanges: stateChanges,
		cb:           cb,
		Logger:       logger,
	}

	// setup an initial connection
	s.createTunnelClientSession(logger)

	return s
}

func (s *reconnectingSession) createTunnelClientSession(logger log.Logger) {
	swapper := new(swapRaw)
	s.sessionsMu.Lock()
	tcs := &session{
		swapper: swapper,
		raw:     swapper,
		Logger:  newLogger(logger),
		// a leg can now be bound onto before its first restartBinds, which
		// used to be what created this map
		tunnels:   make(map[string]*tunnel),
		legNumber: uint32(len(s.sessions)),
	}
	s.sessions = append(s.sessions, tcs)
	s.sessionsMu.Unlock()

	go func() {
		err := s.connect(nil, tcs)
		if err != nil {
			return
		}
		s.receive(tcs)
	}()
}

// legs returns a snapshot so callers never hold sessionsMu across a network
// call to a leg.
func (s *reconnectingSession) legs() []*session {
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()
	return append([]*session(nil), s.sessions...)
}

// firstReadySession returns the lowest-numbered leg that is currently
// connected. An unreachable region must not deny service to the legs that did
// connect, so no specific leg is required.
func (s *reconnectingSession) firstReadySession() *session {
	for _, sess := range s.legs() {
		if sess.swapper.ready() {
			return sess
		}
	}
	return nil
}

func (s *reconnectingSession) firstSession() *session {
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()
	if len(s.sessions) == 0 {
		return nil
	}
	return s.sessions[0]
}

func (s *reconnectingSession) Heartbeat() (time.Duration, error) {
	if sess := s.firstSession(); sess != nil {
		return sess.Heartbeat()
	}
	return 0, ErrSessionNotReady
}

func (s *reconnectingSession) Latency() <-chan time.Duration {
	if sess := s.firstSession(); sess != nil {
		return sess.Latency()
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
	primary := s.firstReadySession()
	if primary == nil {
		return nil, ErrSessionNotReady
	}

	tun, err := listen(primary)
	if err != nil {
		return nil, err
	}

	for _, session := range s.legs() {
		if session == primary {
			continue
		}
		s.attachTunnelToLeg(session, tun.(*tunnel))
	}
	return tun, nil
}

// attachTunnelToLeg mirrors an already-bound tunnel onto a secondary leg.
// Failure is logged and dropped: the endpoint is already serving on the
// primary leg, and tearing it down because a second region is unreachable
// would turn a degraded session into an outage.
func (s *reconnectingSession) attachTunnelToLeg(session *session, tun *tunnel) {
	if !session.swapper.ready() {
		return
	}
	if err := s.reconnectTunnelToSession(session.raw, tun, make(map[string]*tunnel), tun.ID()); err != nil {
		s.Error("failed to attach tunnel to leg, continuing without it",
			"leg", session.legNumber, "id", tun.ID(), "err", err)
		return
	}
	// use locking method
	session.addTunnel(tun.ID(), tun)
}

// adoptBindsFromServingLeg gives a leg that has just joined the tunnel set of
// a leg that is already serving. A leg that was unreachable while endpoints
// were bound has nothing of its own to re-establish, so without this it stays
// idle until some other leg happens to reconnect.
func (s *reconnectingSession) adoptBindsFromServingLeg(target *session) {
	target.RLock()
	empty := len(target.tunnels) == 0
	target.RUnlock()
	if !empty {
		return
	}

	for _, leg := range s.legs() {
		if leg == target || !leg.swapper.ready() {
			continue
		}
		leg.RLock()
		adopted := make(map[string]*tunnel, len(leg.tunnels))
		for id, t := range leg.tunnels {
			adopted[id] = t
		}
		leg.RUnlock()
		if len(adopted) == 0 {
			continue
		}
		target.Lock()
		target.tunnels = adopted
		target.Unlock()
		return
	}
}

func (s *reconnectingSession) SrvInfo() (resp proto.SrvInfoResp, err error) {
	if sess := s.firstSession(); sess != nil {
		return sess.SrvInfo()
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
	var err error
	for _, session := range s.legs() {
		serr := session.Close()
		if serr != nil {
			err = serr
		}
	}
	return err
}

func (s *reconnectingSession) CloseTunnel(clientID string, e error) error {
	var err error
	for _, session := range s.legs() {
		serr := session.CloseTunnel(clientID, e)
		if serr != nil {
			err = serr
		}
	}
	return err
}

func (s *reconnectingSession) receive(session *session) {
	// when we shut down, close all of the open tunnels
	defer func() {
		session.RLock()
		for _, t := range session.tunnels {
			go t.Close()
		}
		session.RUnlock()
	}()

	for {
		// accept the next proxy connection
		proxy, err := session.raw.Accept()
		if err == nil {
			go session.handleProxy(proxy)
			continue
		}

		// we disconnected, reconnect
		err = s.connect(err, session)
		if err != nil {
			session.Info("accept failed", "err", err)
			// permanent failure
			return
		}
	}
}

func (s *reconnectingSession) Auth(extra proto.AuthExtra) (resp proto.AuthResp, err error) {
	legs := s.legs()
	if len(legs) < int(extra.LegNumber) {
		err = errors.New("leg number out of range")
		return
	}
	resp, err = legs[extra.LegNumber].raw.Auth(s.clientID, extra)
	if err != nil {
		return
	}
	if resp.Error != "" {
		err = proto.StringError(resp.Error)
		return
	}
	s.clientID = resp.ClientID
	return
}

func (s *reconnectingSession) connect(acceptErr error, connSession *session) error {
	boff := &backoff.Backoff{
		Min:    500 * time.Millisecond,
		Max:    30 * time.Second,
		Factor: 2,
		Jitter: false,
	}

	failTemp := func(err error, raw RawSession) {
		s.Error("failed to reconnect session", "err", err)
		s.stateChanges <- err

		// the leg is down for as long as we are backing off, so it must not be
		// picked to bind on
		connSession.swapper.clear()

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

	restartBinds := func(session *session) (err error) {
		session.Lock()
		defer session.Unlock()
		raw := session.raw

		// reconnected tunnels, which may have different IDs
		newTunnels := make(map[string]*tunnel, len(session.tunnels))
		for oldID, t := range session.tunnels {
			if err := s.reconnectTunnelToSession(raw, t, newTunnels, oldID); err != nil {
				return err
			}
		}
		session.tunnels = newTunnels
		return nil
	}

	if acceptErr != nil {
		// the connection behind this leg is gone, so stop advertising it as
		// usable until the dial below succeeds
		connSession.swapper.clear()
		if atomic.LoadInt32(&s.closed) == 0 {
			connSession.Error("session closed, starting reconnect loop", "err", acceptErr)
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
		raw, err := s.dialer(connSession.legNumber)
		if err != nil {
			failTemp(err, raw)
			continue
		}

		// successfully reconnected
		connSession.swapper.set(raw)

		// callback for authentication
		desiredLegs, err := s.cb(s, raw, connSession.legNumber)
		if err != nil {
			failTemp(err, raw)
			continue
		}

		// spawn every remaining leg at once. Chaining them meant an
		// unreachable region stalled the legs queued behind it.
		s.sessionsMu.RLock()
		missing := desiredLegs - len(s.sessions)
		s.sessionsMu.RUnlock()
		for i := 0; i < missing; i++ {
			s.createTunnelClientSession(s)
		}

		// a leg that joins after endpoints are already serving carries no
		// binds of its own, so take them from a leg that has them.
		s.adoptBindsFromServingLeg(connSession)

		// re-establish binds
		err = restartBinds(connSession)
		if err != nil {
			failTemp(err, raw)
			continue
		}

		// reset wait
		boff.Reset()

		// any leg reaching this point can serve traffic, so report the
		// session as established rather than waiting for every leg. A region
		// the client cannot reach must not hold up the ones it can.
		s.Info("client session established", "leg", connSession.legNumber)
		s.stateChanges <- nil
		return nil
	}
}

func (s *reconnectingSession) reconnectTunnelToSession(raw RawSession, t *tunnel, newTunnels map[string]*tunnel, oldID string) error {
	// set the returned token for reconnection
	tCfg := t.RemoteBindConfig()
	t.bindExtra.Token = tCfg.Token

	var respErr string
	if tCfg.Labels != nil {
		resp, err := raw.ListenLabel(tCfg.Labels, tCfg.Metadata, t.ForwardsTo(), t.ForwardsProto())
		if err != nil {
			return err
		}
		respErr = resp.Error
		if resp.ID != "" {
			t.id.Store(resp.ID)
			newTunnels[resp.ID] = t
		} else {
			newTunnels[oldID] = t
		}
	} else {
		resp, err := raw.Listen(tCfg.ConfigProto, tCfg.Opts, t.bindExtra, t.ID(), t.ForwardsTo(), t.ForwardsProto())
		if err != nil {
			return err
		}
		respErr = resp.Error

		newTunnels[oldID] = t
	}

	if respErr != "" {
		return errors.New(respErr)
	}
	return nil
}
