package client

import (
	"errors"
	"sync/atomic"
	"time"
	"unsafe"

	log "github.com/inconshreveable/log15"
	"github.com/jpillora/backoff"
	"github.com/ngrok/libngrok-go/internal/tunnel/netx"
	"github.com/ngrok/libngrok-go/internal/tunnel/proto"
)

var ErrSessionNotReady = errors.New("an ngrok tunnel session has not yet been established")

// Wraps a RawSession so that it can be safely swapped out
type swapRaw struct {
	raw unsafe.Pointer
}

func (s *swapRaw) get() RawSession {
	ptr := atomic.LoadPointer(&s.raw)
	if ptr == nil {
		return nil
	}
	return *(*RawSession)(ptr)
}

func (s *swapRaw) set(raw RawSession) {
	atomic.StorePointer(&s.raw, unsafe.Pointer(&raw))
}

func (s *swapRaw) Auth(id string, extra proto.AuthExtra) (resp proto.AuthResp, err error) {
	if raw := s.get(); raw != nil {
		return raw.Auth(id, extra)
	}
	return proto.AuthResp{}, ErrSessionNotReady
}

func (s *swapRaw) Listen(protocol string, opts any, extra proto.BindExtra, id string, forwardsTo string) (resp proto.BindResp, err error) {
	if raw := s.get(); raw != nil {
		return raw.Listen(protocol, opts, extra, id, forwardsTo)
	}
	return proto.BindResp{}, ErrSessionNotReady
}

func (s *swapRaw) ListenLabel(labels map[string]string, metadata string, forwardsTo string) (resp proto.StartTunnelWithLabelResp, err error) {
	if raw := s.get(); raw != nil {
		return raw.ListenLabel(labels, metadata, forwardsTo)
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
	swapper      *swapRaw
	*session
}

type RawSessionDialer func() (RawSession, error)
type ReconnectCallback func(s Session) error

// Establish a Session that reconnects across temporary network failures. The
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
func NewReconnectingSession(logger log.Logger, dialer RawSessionDialer, stateChanges chan<- error, cb ReconnectCallback) Session {
	swapper := new(swapRaw)
	s := &reconnectingSession{
		dialer:       dialer,
		stateChanges: stateChanges,
		cb:           cb,
		swapper:      swapper,
		session: &session{
			tunnels: make(map[string]*tunnel),
			raw:     swapper,
			Logger:  newLogger(logger),
		},
	}

	// setup an initial connection
	go func() {
		err := s.connect(nil)
		if err != nil {
			return
		}
		s.receive()
	}()

	return s
}

func (s *reconnectingSession) Close() error {
	atomic.StoreInt32(&s.closed, 1)
	return s.session.Close()
}

func (s *reconnectingSession) receive() {
	// when we shut down, close all of the open tunnels
	defer func() {
		s.RLock()
		for _, t := range s.tunnels {
			go t.Close()
		}
		s.RUnlock()
	}()

	for {
		// accept the next proxy connection
		proxy, err := s.raw.Accept()
		if err == nil {
			go s.handleProxy(proxy)
			continue
		}

		// we disconnected, reconnect
		err = s.connect(err)
		if err != nil {
			s.Info("accept failed", "err", err)
			// permanent failure
			return
		}
	}
}

func (s *reconnectingSession) Auth(extra proto.AuthExtra) (resp proto.AuthResp, err error) {
	resp, err = s.raw.Auth(s.clientID, extra)
	if err != nil {
		return
	}
	if resp.Error != "" {
		err = errors.New(resp.Error)
		return
	}
	s.clientID = resp.ClientID
	return
}

func (s *reconnectingSession) connect(acceptErr error) error {
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
		s.stateChanges <- err
		close(s.stateChanges)
		return err
	}

	restartBinds := func(raw RawSession) (err error) {
		s.Lock()
		defer s.Unlock()

		// reconnected tunnels, which may have different IDs
		newTunnels := make(map[string]*tunnel, len(s.tunnels))
		for oldID, t := range s.tunnels {
			// set the returned token for reconnection
			tCfg := t.RemoteBindConfig()
			t.bindExtra.Token = tCfg.Token

			var respErr string
			if tCfg.Labels != nil {
				resp, err := raw.ListenLabel(tCfg.Labels, tCfg.Metadata, t.ForwardsTo())
				if err != nil {
					return err
				}
				respErr = resp.Error
				if resp.ID != "" {
					t.id.Store(resp.ID)
					newTunnels[resp.ID] = t
				} else {
					// Otherwise save the old tunnel I guess? Maybe next reconnect gets it?
					// This doesn't seem quite right though...
					newTunnels[oldID] = t
				}
			} else {
				resp, err := raw.Listen(tCfg.ConfigProto, tCfg.Opts, t.bindExtra, t.ID(), t.ForwardsTo())
				if err != nil {
					return err
				}
				respErr = resp.Error
				// same ID, no need to change
				newTunnels[oldID] = t
			}

			if respErr != "" {
				return errors.New(respErr)
			}
		}
		s.tunnels = newTunnels
		return nil
	}

	if acceptErr != nil {
		if atomic.LoadInt32(&s.closed) == 0 {
			s.Error("session closed, starting reconnect loop", "err", acceptErr)
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
		raw, err := s.dialer()
		if err != nil {
			failTemp(err, raw)
			continue
		}

		// successfully reconnected
		s.swapper.set(raw)

		// callback for authentication
		if err := s.cb(s); err != nil {
			failTemp(err, raw)
			continue
		}

		// re-establish binds
		err = restartBinds(raw)
		if err != nil {
			failTemp(err, raw)
			continue
		}

		// reset wait
		boff.Reset()

		s.Info("client session established")
		s.stateChanges <- nil
		return nil
	}
}
