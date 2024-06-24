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
	closed            int32
	dialer            RawSessionDialer
	stateChanges      chan<- error
	clientID          string
	cb                ReconnectCallback
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
	tcs := &session{
		swapper:   swapper,
		raw:       swapper,
		Logger:    newLogger(logger),
		legNumber: uint32(len(s.sessions)),
	}
	s.sessions = append(s.sessions, tcs)

	go func() {
		err := s.connect(nil, tcs)
		if err != nil {
			return
		}
		s.receive(tcs)
	}()
}

func (s *reconnectingSession) firstSession() *session {
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
	if sess := s.firstSession(); sess != nil {
		tun, err := listen(sess)
		if err != nil {
			return nil, err
		}
		// connect this tunnel to the other legs
		for _, session := range s.sessions[1:] {
			if e := s.reconnectTunnelToSession(session.raw, tun.(*tunnel), make(map[string]*tunnel), tun.ID()); e != nil {
				return nil, e
			}
			// use locking method
			session.addTunnel(tun.ID(), tun.(*tunnel))
		}
		return tun, nil
	}
	return nil, ErrSessionNotReady
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
	for _, session := range s.sessions {
		serr := session.Close()
		if serr != nil {
			err = serr
		}
	}
	return err
}

func (s *reconnectingSession) CloseTunnel(clientID string, e error) error {
	var err error
	for _, session := range s.sessions {
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
	if len(s.sessions) < int(extra.LegNumber) {
		err = errors.New("leg number out of range")
		return
	}
	resp, err = s.sessions[extra.LegNumber].raw.Auth(s.clientID, extra)
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

		// check if more sessions need to be established
		sendStateChange := true
		if desiredLegs > len(s.sessions) {
			// set up the next connection. additional sessions will
			// continue to chain on from there until all legs are
			// established
			s.createTunnelClientSession(s)
			// not done with initial setup yet
			sendStateChange = false
		}

		// re-establish binds
		err = restartBinds(connSession)
		if err != nil {
			failTemp(err, raw)
			continue
		}

		if sendStateChange {
			// reset wait
			boff.Reset()

			s.Info("client session established")
			s.stateChanges <- nil
		}
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
