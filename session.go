package libngrok

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"net/url"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/inconshreveable/log15"
	"github.com/ngrok/libngrok-go/internal/muxado"
	tunnel_client "github.com/ngrok/libngrok-go/internal/tunnel/client"
	"github.com/ngrok/libngrok-go/internal/tunnel/proto"
	"github.com/ngrok/libngrok-go/log"
	"golang.org/x/net/proxy"
)

const VERSION = "4.0.0-library"

type Session interface {
	Close() error

	StartTunnel(ctx context.Context, cfg TunnelConfig) (Tunnel, error)

	SrvInfo() (SrvInfo, error)
	AuthResp() AuthResp

	Heartbeat() (time.Duration, error)

	Latency() <-chan time.Duration
}

const (
	RegionUS = "us"
	RegionEU = "eu"
	RegionSA = "sa"
	RegionAP = "ap"
	RegionAU = "au"
	RegionJP = "jp"
	RegionIN = "in"
)

//go:embed ngrok.ca.crt
var defaultCACert []byte

const defaultServer = "tunnel.ngrok.com:443"

type Dialer interface {
	Dial(network, address string) (net.Conn, error)
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Callbacks in response to local(ish) network events.
type LocalCallbacks struct {
	// Called any time a session (re)connects.
	OnConnect func(ctx context.Context, sess Session)
	// Called any time a session disconnects.
	// If the session has been closed locally, `OnDisconnect` will be called a
	// final time with a `nil` `err`.
	OnDisconnect func(ctx context.Context, sess Session, err error)
	// Called any time an automatic heartbeat response is received.
	// This does not include on-demand heartbeating via the `Session.Heartbeat`
	// method.
	OnHeartbeat func(ctx context.Context, sess Session, latency time.Duration)
}

// Callbacks in response to remote requests
type RemoteCallbacks struct {
	// Called when a stop is requested via the dashboard or API.
	// If it returns nil, success will be reported and the session closed.
	OnStop func(ctx context.Context, sess Session) error
	// Called when a restart is requested via the dashboard or API.
	// If it returns nil, success will be reported and the session closed.
	// It is the implementer's responsibility to ensure that the application
	// recreates the session.
	OnRestart func(ctx context.Context, sess Session) error
	// Called when an update is requested via the dashboard or API.
	// If it returns nil, success will be reported. Any other semantics are left
	// up to the application, as automatic library updates aren't possible.
	OnUpdate func(ctx context.Context, sess Session) error
}

type CallbackErrors struct {
	UpdateUnsupported  string
	RestartUnsupported string
	StopUnsupported    string
}

type ConnectConfig struct {
	AuthToken  string
	ServerAddr string
	CAPool     *x509.CertPool

	Dialer Dialer

	Resolver *net.Resolver
	ProxyURL *url.URL

	Metadata string

	HeartbeatConfig *muxado.HeartbeatConfig

	LocalCallbacks  LocalCallbacks
	RemoteCallbacks RemoteCallbacks

	CallbackErrors CallbackErrors

	Cookie string

	Logger log15.Logger
}

func ConnectOptions() *ConnectConfig {
	return &ConnectConfig{}
}

func (cfg *ConnectConfig) WithMetadata(meta string) *ConnectConfig {
	cfg.Metadata = meta
	return cfg
}

func (cfg *ConnectConfig) WithDialer(dialer Dialer) *ConnectConfig {
	cfg.Dialer = dialer
	return cfg
}

func (cfg *ConnectConfig) WithProxyURL(url *url.URL) *ConnectConfig {
	cfg.ProxyURL = url
	return cfg
}

func (cfg *ConnectConfig) WithResolver(resolver *net.Resolver) *ConnectConfig {
	cfg.Resolver = resolver
	return cfg
}

func (cfg *ConnectConfig) WithAuthToken(token string) *ConnectConfig {
	cfg.AuthToken = token
	return cfg
}

func (cfg *ConnectConfig) WithRegion(region string) *ConnectConfig {
	if region != "" {
		cfg.ServerAddr = fmt.Sprintf("tunnel.%s.ngrok.com:443", region)
	}
	return cfg
}

func (cfg *ConnectConfig) WithServer(addr string) *ConnectConfig {
	cfg.ServerAddr = addr
	return cfg
}

func (cfg *ConnectConfig) WithCA(pool *x509.CertPool) *ConnectConfig {
	cfg.CAPool = pool
	return cfg
}

func (cfg *ConnectConfig) WithHeartbeatTolerance(tolerance time.Duration) *ConnectConfig {
	if cfg.HeartbeatConfig == nil {
		cfg.HeartbeatConfig = muxado.NewHeartbeatConfig()
	}
	cfg.HeartbeatConfig.Tolerance = tolerance
	return cfg
}

func (cfg *ConnectConfig) WithHeartbeatInterval(interval time.Duration) *ConnectConfig {
	if cfg.HeartbeatConfig == nil {
		cfg.HeartbeatConfig = muxado.NewHeartbeatConfig()
	}
	cfg.HeartbeatConfig.Interval = interval
	return cfg
}

// Log to a log15.Logger.
// This is the logging interface that the internals use, so this is the most
// direct way to set a logger.
func (cfg *ConnectConfig) WithLog15(logger log15.Logger) *ConnectConfig {
	cfg.Logger = logger
	return cfg
}

// Log to a simplified logging interface.
// This is a "lowest common denominator" interface that should be simple to
// adapt other loggers to. Examples are provided in `log15adapter` and
// `pgxadapter`.
// If the provided `Logger` also implements the `log15.Logger` interface, it
// will be used directly.
func (cfg *ConnectConfig) WithLogger(logger log.Logger) *ConnectConfig {
	cfg.Logger = toLog15(logger)
	return cfg
}

func (cfg *ConnectConfig) WithLocalCallbacks(callbacks LocalCallbacks) *ConnectConfig {
	cfg.LocalCallbacks = callbacks
	return cfg
}

func (cfg *ConnectConfig) WithRemoteCallbacks(callbacks RemoteCallbacks) *ConnectConfig {
	cfg.RemoteCallbacks = callbacks
	return cfg
}

func (cfg *ConnectConfig) WithReconnectCookie(cookie string) *ConnectConfig {
	cfg.Cookie = cookie
	return cfg
}

func (cfg *ConnectConfig) WithCallbackErrors(errs CallbackErrors) *ConnectConfig {
	cfg.CallbackErrors = errs
	return cfg
}

func Connect(ctx context.Context, cfg *ConnectConfig) (Session, error) {
	if cfg.Logger == nil {
		cfg.Logger = log15.New()
		cfg.Logger.SetHandler(log15.DiscardHandler())
	}

	if cfg.CAPool == nil {
		cfg.CAPool = x509.NewCertPool()
		cfg.CAPool.AppendCertsFromPEM(defaultCACert)
	}

	if cfg.ServerAddr == "" {
		cfg.ServerAddr = defaultServer
	}

	tlsConfig := &tls.Config{
		RootCAs:    cfg.CAPool,
		ServerName: strings.Split(cfg.ServerAddr, ":")[0],
		MinVersion: tls.VersionTLS12,
	}

	var dialer Dialer

	if cfg.Dialer != nil {
		dialer = cfg.Dialer
	} else {
		netDialer := &net.Dialer{
			Resolver: cfg.Resolver,
		}

		if cfg.ProxyURL != nil {
			proxied, err := proxy.FromURL(cfg.ProxyURL, netDialer)
			if err != nil {
				return nil, ErrProxyInit{err, ProxyInitContext{cfg.ProxyURL}}
			}
			dialer = proxied.(Dialer)
		} else {
			dialer = netDialer
		}
	}

	if cfg.HeartbeatConfig == nil {
		cfg.HeartbeatConfig = muxado.NewHeartbeatConfig()
	}

	session := new(sessionImpl)

	stateChanges := make(chan error, 32)

	callbackHandler := remoteCallbackHandler{
		Logger: cfg.Logger,
		sess:   session,
		cb:     cfg.RemoteCallbacks,
	}

	rawDialer := func() (tunnel_client.RawSession, error) {
		conn, err := dialer.DialContext(ctx, "tcp", cfg.ServerAddr)
		if err != nil {
			return nil, ErrSessionDial{err, DialContext{cfg.ServerAddr}}
		}

		conn = tls.Client(conn, tlsConfig)

		sess := muxado.Client(conn, &muxado.Config{})
		return tunnel_client.NewRawSession(cfg.Logger, sess, cfg.HeartbeatConfig, callbackHandler), nil
	}

	empty := ""
	notImplemented := "not implemented"

	var remoteStopErr, remoteRestartErr, remoteUpdateErr = &notImplemented, &notImplemented, &notImplemented

	if cfg.CallbackErrors.StopUnsupported != "" {
		remoteStopErr = &cfg.CallbackErrors.StopUnsupported
	}
	if cfg.CallbackErrors.UpdateUnsupported != "" {
		remoteUpdateErr = &cfg.CallbackErrors.UpdateUnsupported
	}
	if cfg.CallbackErrors.RestartUnsupported != "" {
		remoteRestartErr = &cfg.CallbackErrors.RestartUnsupported
	}

	if cfg.RemoteCallbacks.OnStop != nil {
		remoteStopErr = &empty
	}
	if cfg.RemoteCallbacks.OnRestart != nil {
		remoteRestartErr = &empty
	}
	if cfg.RemoteCallbacks.OnUpdate != nil {
		remoteUpdateErr = &empty
	}

	auth := proto.AuthExtra{
		Version:            VERSION,
		Authtoken:          cfg.AuthToken,
		Metadata:           cfg.Metadata,
		OS:                 runtime.GOOS,
		Arch:               runtime.GOARCH,
		HeartbeatInterval:  int64(cfg.HeartbeatConfig.Interval),
		HeartbeatTolerance: int64(cfg.HeartbeatConfig.Tolerance),

		RestartUnsupportedError: remoteRestartErr,
		StopUnsupportedError:    remoteStopErr,
		UpdateUnsupportedError:  remoteUpdateErr,

		Cookie: cfg.Cookie,

		// TODO: More fields here?
	}

	reconnect := func(sess tunnel_client.Session) error {
		resp, err := sess.Auth(auth)
		if err != nil {
			remote := false
			if resp.Error != "" {
				err = errors.New(resp.Error)
				remote = true
			}
			return ErrAuthFailed{err, AuthFailedContext{remote}}
		}

		session.setInner(&sessionInner{
			Session:  sess,
			AuthResp: resp,
		})

		if cfg.LocalCallbacks.OnHeartbeat != nil {
			go func() {
				beats := session.Latency()
				for {
					select {
					case <-ctx.Done():
						return
					case latency, ok := <-beats:
						if !ok {
							return
						}
						cfg.LocalCallbacks.OnHeartbeat(ctx, session, latency)
					}
				}
			}()
		}

		auth.Cookie = resp.Extra.Cookie
		return nil
	}

	sess := tunnel_client.NewReconnectingSession(cfg.Logger, rawDialer, stateChanges, reconnect)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-stateChanges:
		if err != nil {
			sess.Close()
			return nil, err
		}
	}

	if cfg.LocalCallbacks.OnConnect != nil {
		cfg.LocalCallbacks.OnConnect(ctx, session)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case err, ok := <-stateChanges:
				if !ok {
					if cfg.LocalCallbacks.OnDisconnect != nil {
						cfg.Logger.Info("no more state changes")
						cfg.LocalCallbacks.OnDisconnect(ctx, session, nil)
					}
					return
				}
				if err == nil && cfg.LocalCallbacks.OnConnect != nil {
					cfg.LocalCallbacks.OnConnect(ctx, session)
				}
				if err != nil && cfg.LocalCallbacks.OnDisconnect != nil {
					cfg.LocalCallbacks.OnDisconnect(ctx, session, err)
				}
			}
		}
	}()

	return session, nil
}

type sessionImpl struct {
	raw unsafe.Pointer
}

type sessionInner struct {
	tunnel_client.Session
	AuthResp proto.AuthResp
}

func (s *sessionImpl) inner() *sessionInner {
	ptr := atomic.LoadPointer(&s.raw)
	if ptr == nil {
		return nil
	}
	return (*sessionInner)(ptr)
}

func (s *sessionImpl) setInner(raw *sessionInner) {
	atomic.StorePointer(&s.raw, unsafe.Pointer(raw))
}

func (s *sessionImpl) Close() error {
	return s.inner().Close()
}

func (s *sessionImpl) StartTunnel(ctx context.Context, cfg TunnelConfig) (Tunnel, error) {
	var (
		tunnel tunnel_client.Tunnel
		err    error
	)

	tunnelCfg := cfg.tunnelConfig()

	if tunnelCfg.proto != "" {
		tunnel, err = s.inner().Listen(tunnelCfg.proto, tunnelCfg.opts, tunnelCfg.extra, tunnelCfg.forwardsTo)
	} else {
		tunnel, err = s.inner().ListenLabel(tunnelCfg.labels, tunnelCfg.extra.Metadata, tunnelCfg.forwardsTo)
	}

	if err != nil {
		return nil, ErrStartTunnel{err, StartContext{
			Config: cfg,
		}}
	}

	return &tunnelImpl{
		Tunnel: tunnel,
	}, nil
}

type SrvInfo proto.SrvInfoResp
type AuthResp proto.AuthResp

func (s *sessionImpl) AuthResp() AuthResp {
	return AuthResp(s.inner().AuthResp)
}

func (s *sessionImpl) SrvInfo() (SrvInfo, error) {
	resp, err := s.inner().SrvInfo()
	return SrvInfo(resp), err
}

func (s *sessionImpl) Heartbeat() (time.Duration, error) {
	return s.inner().Heartbeat()
}

func (s *sessionImpl) Latency() <-chan time.Duration {
	return s.inner().Latency()
}

type remoteCallbackHandler struct {
	log15.Logger
	sess Session
	cb   RemoteCallbacks
}

func (rc remoteCallbackHandler) OnStop(_ *proto.Stop, respond tunnel_client.HandlerRespFunc) {
	if rc.cb.OnStop != nil {
		resp := new(proto.StopResp)
		close := true
		if err := rc.cb.OnStop(context.TODO(), rc.sess); err != nil {
			close = false
			resp.Error = err.Error()
		}
		if err := respond(resp); err != nil {
			rc.Warn("error responding to stop request", "error", err)
		}
		if close {
			_ = rc.sess.Close()
		}
	}
}

func (rc remoteCallbackHandler) OnRestart(_ *proto.Restart, respond tunnel_client.HandlerRespFunc) {
	if rc.cb.OnRestart != nil {
		resp := new(proto.RestartResp)
		close := true
		if err := rc.cb.OnRestart(context.TODO(), rc.sess); err != nil {
			close = false
			resp.Error = err.Error()
		}
		if err := respond(resp); err != nil {
			rc.Warn("error responding to restart request", "error", err)
		}
		if close {
			_ = rc.sess.Close()
		}
	}
}

func (rc remoteCallbackHandler) OnUpdate(_ *proto.Update, respond tunnel_client.HandlerRespFunc) {
	if rc.cb.OnUpdate != nil {
		resp := new(proto.UpdateResp)
		if err := rc.cb.OnUpdate(context.TODO(), rc.sess); err != nil {
			resp.Error = err.Error()
		}
		if err := respond(resp); err != nil {
			rc.Warn("error responding to restart request", "error", err)
		}
	}
}
