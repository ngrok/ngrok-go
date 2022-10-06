package ngrok

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
	"github.com/ngrok/ngrok-go/internal/muxado"
	tunnel_client "github.com/ngrok/ngrok-go/internal/tunnel/client"
	"github.com/ngrok/ngrok-go/internal/tunnel/proto"
	"golang.org/x/net/proxy"
)

// The ngrok library version.
const VERSION = "4.0.0-library"

// The interface implemented by an ngrok session object.
type Session interface {
	// Close the ngrok session.
	// This also closes all existing tunnels tied to the session.
	Close() error

	// Start a new tunnel over the ngrok session.
	StartTunnel(ctx context.Context, cfg TunnelConfig) (Tunnel, error)
}

const (
	// The US ngrok region.
	RegionUS = "us"
	// The Europe ngrok region.
	RegionEU = "eu"
	// The South America ngrok region.
	RegionSA = "sa"
	// The Asia-Pacific ngrok region.
	RegionAP = "ap"
	// The Australia ngrok region.
	RegionAU = "au"
	// The Japan ngrok region.
	RegionJP = "jp"
	// The India ngrok region.
	RegionIN = "in"
)

//go:embed assets/ngrok.ca.crt
var defaultCACert []byte

const defaultServer = "tunnel.ngrok.com:443"

// Interface implemented by supported dialers for establishing a connection to
// the ngrok server.
type Dialer interface {
	// Connect to an address on the named network.
	// See the documentation for [net.Dial].
	Dial(network, address string) (net.Conn, error)
	// Connect to an address on the named network with the provided
	// [context.Context].
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

// Options to use when establishing an ngrok session.
type ConnectConfig struct {
	// Your ngrok Authtoken.
	Authtoken string
	// The address of the ngrok server to connect to.
	// Defaults to `tunnel.ngrok.com:443`
	ServerAddr string
	// The [x509.CertPool] used to authenticate the ngrok server certificate.
	CAPool *x509.CertPool

	// The [Dialer] used to establish the initial TCP connection to the ngrok
	// server.
	// If set, takes precedence over Resolver and ProxyURL settings.
	// If not set, defaults to a [net.Dialer].
	Dialer Dialer

	// The DNS resolver configuration to use with the default [Dialer].
	Resolver *net.Resolver
	// The URL of a proxy to use when making the TCP connection to the ngrok
	// server.
	// Any proxy supported by [golang.org/x/net/proxy] may be used.
	ProxyURL *url.URL

	// Opaque metadata string to be associated with the session.
	// Viewable from the ngrok dashboard or API.
	Metadata string

	// Configuration for the session's heartbeat.
	// TODO(josh): don't expose muxado in the public API
	HeartbeatConfig *muxado.HeartbeatConfig

	// Callbacks for local network events.
	LocalCallbacks LocalCallbacks
	// Callbacks for remote requests.
	RemoteCallbacks RemoteCallbacks

	// The logger for the session to use.
	Logger log15.Logger
}

// Construct a new set of Connect options.
func ConnectOptions() *ConnectConfig {
	return &ConnectConfig{}
}

// Use the provided opaque metadata string for this session.
// Sets the [ConnectConfig].Metadata field.
func (cfg *ConnectConfig) WithMetadata(meta string) *ConnectConfig {
	cfg.Metadata = meta
	return cfg
}

// Use the provided dialer for establishing a TCP connection to the ngrok
// server.
// Sets the [ConnectConfig].Dialer field and takes precedence over ProxyURL and
// Resolver settings.
func (cfg *ConnectConfig) WithDialer(dialer Dialer) *ConnectConfig {
	cfg.Dialer = dialer
	return cfg
}

// Proxy requests through the server identified by the provided URL when using
// the default Dialer.
// Sets the [ConnectConfig].ProxyURL field. Ignored if a custom Dialer is in use.
func (cfg *ConnectConfig) WithProxyURL(url *url.URL) *ConnectConfig {
	cfg.ProxyURL = url
	return cfg
}

// Use the provided [net.Resolver] settings when using the default Dialer.
// Sets the [ConnectConfig].Resolver field. Ignored if a custom Dialer is in use.
func (cfg *ConnectConfig) WithResolver(resolver *net.Resolver) *ConnectConfig {
	cfg.Resolver = resolver
	return cfg
}

// Use the provided Authtoken to authenticate this session.
// Sets the [ConnectConfig].Authtoken field.
func (cfg *ConnectConfig) WithAuthtoken(token string) *ConnectConfig {
	cfg.Authtoken = token
	return cfg
}

// Connect to the ngrok server in a specific region.
// Overwrites the [ConnectConfig].ServerAddr field.
func (cfg *ConnectConfig) WithRegion(region string) *ConnectConfig {
	if region != "" {
		cfg.ServerAddr = fmt.Sprintf("tunnel.%s.ngrok.com:443", region)
	}
	return cfg
}

// Connect to the provided ngrok server.
// Sets the [ConnectConfig].Server field.
func (cfg *ConnectConfig) WithServer(addr string) *ConnectConfig {
	cfg.ServerAddr = addr
	return cfg
}

// Use the provided [x509.CertPool] to authenticate the ngrok server
// certificate.
// Sets the [ConnectConfig].CAPool field.
func (cfg *ConnectConfig) WithCA(pool *x509.CertPool) *ConnectConfig {
	cfg.CAPool = pool
	return cfg
}

// Set the heartbeat tolerance for the session.
// If the session's heartbeats are outside of their interval by this duration,
// the server will assume the session is dead and close it.
func (cfg *ConnectConfig) WithHeartbeatTolerance(tolerance time.Duration) *ConnectConfig {
	if cfg.HeartbeatConfig == nil {
		cfg.HeartbeatConfig = muxado.NewHeartbeatConfig()
	}
	cfg.HeartbeatConfig.Tolerance = tolerance
	return cfg
}

// Set the heartbeat interval for the session.
// If the session's heartbeats are outside of this interval by the heartbeat
// tolerance, the server will assume the session is dead and close it.
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
func (cfg *ConnectConfig) WithLogger(logger Logger) *ConnectConfig {
	cfg.Logger = toLog15(logger)
	return cfg
}

// Set the callbacks for local network events.
func (cfg *ConnectConfig) WithLocalCallbacks(callbacks LocalCallbacks) *ConnectConfig {
	cfg.LocalCallbacks = callbacks
	return cfg
}

// Set the callbacks for requests from the ngrok dashboard.
func (cfg *ConnectConfig) WithRemoteCallbacks(callbacks RemoteCallbacks) *ConnectConfig {
	cfg.RemoteCallbacks = callbacks
	return cfg
}

// Connect to the ngrok server and start a new session.
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
				return nil, ErrProxyInit{cfg.ProxyURL, err}
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
			return nil, ErrSessionDial{cfg.ServerAddr, err}
		}

		conn = tls.Client(conn, tlsConfig)

		sess := muxado.Client(conn, &muxado.Config{})
		return tunnel_client.NewRawSession(cfg.Logger, sess, cfg.HeartbeatConfig, callbackHandler), nil
	}

	empty := ""
	notImplemented := "the agent has not defined a callback for this operation"

	var remoteStopErr, remoteRestartErr, remoteUpdateErr = &notImplemented, &notImplemented, &notImplemented

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
		Authtoken:          cfg.Authtoken,
		Metadata:           cfg.Metadata,
		OS:                 runtime.GOOS,
		Arch:               runtime.GOARCH,
		HeartbeatInterval:  int64(cfg.HeartbeatConfig.Interval),
		HeartbeatTolerance: int64(cfg.HeartbeatConfig.Tolerance),

		RestartUnsupportedError: remoteRestartErr,
		StopUnsupportedError:    remoteStopErr,
		UpdateUnsupportedError:  remoteUpdateErr,

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
			return ErrAuthFailed{remote, err}
		}

		session.setInner(&sessionInner{
			Session:         sess,
			Region:          resp.Extra.Region,
			ProtoVersion:    resp.Version,
			ServerVersion:   resp.Extra.Version,
			ClientID:        resp.Extra.Region,
			AccountName:     resp.Extra.AccountName,
			PlanName:        resp.Extra.PlanName,
			Banner:          resp.Extra.Banner,
			SessionDuration: resp.Extra.SessionDuration,
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

	Region          string
	ProtoVersion    string
	ServerVersion   string
	ClientID        string
	AccountName     string
	PlanName        string
	Banner          string
	SessionDuration int64
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

	tunnelCfg := &tunnelConfig{}
	cfg.applyTunnelConfig(tunnelCfg)

	if tunnelCfg.proto != "" {
		tunnel, err = s.inner().Listen(tunnelCfg.proto, tunnelCfg.opts, tunnelCfg.extra, tunnelCfg.forwardsTo)
	} else {
		tunnel, err = s.inner().ListenLabel(tunnelCfg.labels, tunnelCfg.extra.Metadata, tunnelCfg.forwardsTo)
	}

	if err != nil {
		return nil, ErrStartTunnel{cfg, err}
	}

	return &tunnelImpl{
		Tunnel: tunnel,
	}, nil
}

// The rest of the `sessionImpl` methods are non-public, but can be
// interface-asserted if they're *really* needed. These are exempt from any
// stability guarantees and subject to change without notice.

func (s *sessionImpl) ProtoVersion() string {
	return s.inner().ProtoVersion
}
func (s *sessionImpl) ServerVersion() string {
	return s.inner().ServerVersion
}
func (s *sessionImpl) ClientID() string {
	return s.inner().ClientID
}
func (s *sessionImpl) AccountName() string {
	return s.inner().AccountName
}
func (s *sessionImpl) PlanName() string {
	return s.inner().PlanName
}
func (s *sessionImpl) Banner() string {
	return s.inner().Banner
}
func (s *sessionImpl) SessionDuration() int64 {
	return s.inner().SessionDuration
}
func (s *sessionImpl) Region() string {
	return s.inner().Region
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
