package ngrok

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/inconshreveable/log15/v3"
	"golang.org/x/net/proxy"

	"golang.ngrok.com/ngrok/config"

	"golang.ngrok.com/ngrok/internal/muxado"
	tunnel_client "golang.ngrok.com/ngrok/internal/tunnel/client"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
	"golang.ngrok.com/ngrok/log"
)

// The ngrok library version.
const libraryAgentVersion = "0.0.0"

// Session encapsulates an established session with the ngrok service. Sessions
// recover from network failures by automatically reconnecting.
type Session interface {
	// Listen creates a new Tunnel which will listen for new inbound
	// connections. The returned Tunnel object is a net.Listener.
	Listen(ctx context.Context, cfg config.Tunnel) (Tunnel, error)

	// Close ends the ngrok session. All Tunnel objects created by Listen
	// on this session will be closed.
	Close() error
}

//go:embed assets/ngrok.ca.crt
var defaultCACert []byte

const defaultServer = "tunnel.ngrok.com:443"

// Dialer is the interface a custom connection dialer must implement for use
// with the [WithDialer] option.
type Dialer interface {
	// Connect to an address on the named network.
	// See the documentation for net.Dial.
	Dial(network, address string) (net.Conn, error)
	// Connect to an address on the named network with the provided
	// context.
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// SessionConnectHandler is the callback type for [WithConnectHandler]
type SessionConnectHandler func(ctx context.Context, sess Session)

// SessionDisconnectHandler is the callback type for [WithDisconnectHandler]
type SessionDisconnectHandler func(ctx context.Context, sess Session, err error)

// SessionHearbeatHandler is the callback type for [WithHearbeatHandler]
type SessionHeartbeatHandler func(ctx context.Context, sess Session, latency time.Duration)

// ServerCommandHandler is the callback type for [WithStopHandler]
type ServerCommandHandler func(ctx context.Context, sess Session) error

// ConnectOptions are passed to [Connect] to customize session connection and
// establishment.
type ConnectOption func(*connectConfig)

// Options to use when establishing an ngrok session.
type connectConfig struct {
	// Your ngrok Authtoken.
	Authtoken proto.ObfuscatedString
	// The address of the ngrok server to connect to.
	// Defaults to `tunnel.ngrok.com:443`
	ServerAddr string
	// The [x509.CertPool] used to authenticate the ngrok server certificate.
	CAPool *x509.CertPool

	// The [Dialer] used to establish the initial TCP connection to the ngrok
	// server.
	// If set, takes precedence over the ProxyURL setting.
	// If not set, defaults to a [net.Dialer].
	Dialer Dialer

	// The URL of a proxy to use when making the TCP connection to the ngrok
	// server.
	// Any proxy supported by [golang.org/x/net/proxy] may be used.
	ProxyURL *url.URL

	// Opaque metadata string to be associated with the session.
	// Viewable from the ngrok dashboard or API.
	Metadata string

	// HeartbeatInterval determines how often we send application level
	// heartbeats to the server go check connection liveness.
	HeartbeatInterval time.Duration
	// HeartbeatTolerance is the duration after which an unacknowledged
	// heartbeat is determined to mean the connection is dead.
	HeartbeatTolerance time.Duration

	ConnectHandler    SessionConnectHandler
	DisconnectHandler SessionDisconnectHandler
	HeartbeatHandler  SessionHeartbeatHandler

	StopHandler    ServerCommandHandler
	RestartHandler ServerCommandHandler
	UpdateHandler  ServerCommandHandler

	// The logger for the session to use.
	Logger log.Logger
}

// WithMetdata configures the opaque, machine-readable metadata string for this
// session. Metadata is made available to you in the ngrok dashboard and the
// Agents API resource. It is a useful way to allow you to uniquely identify
// sessions. We suggest encoding the value in a structured format like JSON.
//
// See the [metdata parameter in the ngrok docs] for additional details.
//
// [metdata parameter in the ngrok docs]: https://ngrok.com/docs/ngrok-agent/config#metadata
func WithMetadata(meta string) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.Metadata = meta
	}
}

// WithDialer configures the session to use the provided [Dialer] when
// establishing a connection to the ngrok service. This option will cause
// [WithProxyURL] to be ignored.
func WithDialer(dialer Dialer) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.Dialer = dialer
	}
}

// WithProxyURL configures the session to connect to ngrok through an outbound
// HTTP or SOCKS5 proxy. This parameter is ignored if you override the dialer
// with [WithDialer].
//
// See the [proxy url paramter in the ngrok docs] for additional details.
//
// [proxy url paramter in the ngrok docs]: https://ngrok.com/docs/ngrok-agent/config#proxy_url
func WithProxyURL(url *url.URL) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.ProxyURL = url
	}
}

// WithAuthtoken configures the sesssion to authenticate with the provided
// authtoken. You can [find your existing authtoken] or [create a new one] in the ngrok dashboard.
//
// See the [authtoken parameter in the ngrok docs] for additional details.
//
// [find your existing authtoken]: https://dashboard.ngrok.com/get-started/your-authtoken
// [create a new one]: https://dashboard.ngrok.com/tunnels/authtokens
// [authtoken parameter in the ngrok docs]: https://ngrok.com/docs/ngrok-agent/config#authtoken
func WithAuthtoken(token string) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.Authtoken = proto.ObfuscatedString(token)
	}
}

// WithAuthtokenFromEnv is a shortcut for calling [WithAuthtoken] with the
// value of the NGROK_AUTHTOKEN environment variable.
func WithAuthtokenFromEnv() ConnectOption {
	return WithAuthtoken(os.Getenv("NGROK_AUTHTOKEN"))
}

// WithRegion configures the session to connect to a specific ngrok region.
// If unspecified, ngrok will connect to the fastest region, which is usually what you want.
// The [full list of ngrok regions] can be found in the ngrok documentation.
//
// See the [region parameter in the ngrok docs] for additional details.
//
// [full list of ngrok regions]: https://ngrok.com/docs/platform/pops
// [region parameter in the ngrok docs]: https://ngrok.com/docs/ngrok-agent/config#region
func WithRegion(region string) ConnectOption {
	return func(cfg *connectConfig) {
		if region != "" {
			cfg.ServerAddr = fmt.Sprintf("tunnel.%s.ngrok.com:443", region)
		}
	}
}

// WithServer configures the network address to dial to connect to the ngrok
// service. Use this option only if you are connecting to a custom agent
// ingress.
//
// See the [server_addr parameter in the ngrok docs] for additional details.
//
// [server_addr parameter in the ngrok docs]: https://ngrok.com/docs/ngrok-agent/config#server_addr
func WithServer(addr string) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.ServerAddr = addr
	}
}

// WithCA configures the CAs used to validate the TLS certificate returned by
// the ngrok service while establishing the session. Use this option only if
// you are connecting through a man-in-the-middle or deep packet inspection
// proxy.
//
// See the [root_cas parameter in the ngrok docs] for additional details.
//
// [root_cas parameter in the ngrok docs]: https://ngrok.com/docs/ngrok-agent/config#root_cas
func WithCA(pool *x509.CertPool) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.CAPool = pool
	}
}

// WithHeartbeatTolerance configures the duration to wait for a response to a heartbeat
// before assuming the session connection is dead and attempting to reconnect.
//
// See the [heartbeat_tolerance parameter in the ngrok docs] for additional details.
//
// [heartbeat_tolerance parameter in the ngrok docs]: https://ngrok.com/docs/ngrok-agent/config#heartbeat_tolerance
func WithHeartbeatTolerance(tolerance time.Duration) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.HeartbeatTolerance = tolerance
	}
}

// WithHeartbeatInterval configures how often the session will send heartbeat
// messages to the ngrok service to check session liveness.
//
// See the [heartbeat_interval parameter in the ngrok docs] for additional details.
//
// [heartbeat_interval parameter in the ngrok docs]: https://ngrok.com/docs/ngrok-agent/config#heartbeat_interval
func WithHeartbeatInterval(interval time.Duration) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.HeartbeatInterval = interval
	}
}

// WithLogger configures a logger to recieve log messages from the [Session]. The
// log subpackage contains adapters for both [logrus] and [zap].
//
// [logrus]: https://pkg.go.dev/github.com/sirupsen/logrus
// [zap]: https://pkg.go.dev/go.uber.org/zap
func WithLogger(logger log.Logger) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.Logger = logger
	}
}

// WithConnectHandler configures a function which is called each time the ngrok
// [Session] successfully connects to the ngrok service. Use this option to
// receive events when ngrok successfully reconnects a [Session] that was
// disconnected because of a network failure.
func WithConnectHandler(handler SessionConnectHandler) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.ConnectHandler = handler
	}
}

// WithDisconnectHandler configures a function which is called each time the
// ngrok [Session] disconnects from the ngrok service. Use this option to detect
// when the ngrok session has gone temporarily offline.
func WithDisconnectHandler(handler SessionDisconnectHandler) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.DisconnectHandler = handler
	}
}

// WithHeartbeatHandler configures a function which is called each time the
// [Session] successfully heartbeats the ngrok service. The callback receives
// the latency of the round trip time from initiating the heartbeat to
// receiving an acknowledgement back from the ngrok service.
func WithHeartbeatHandler(handler SessionHeartbeatHandler) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.HeartbeatHandler = handler
	}
}

// WithStopHandler configures a function which is called when the ngrok service
// requests that this [Session] stops. Your application may choose to interpret
// this callback as a request to terminate the [Session] or the entire process.
//
// Errors returned by this function will be visible to the ngrok dashboard or
// API as the response to the Stop operation.
//
// Do not block inside this callback. It will cause the Dashboard or API stop
// operation to hang. Do not call [Session].Close or [os.Exit] inside this
// callback, it will also cause the operation to hang.
//
// Instead, either return an error or if you intend to Stop, spawn a goroutine
// to asynchronously call [Session].Close or [os.Exit].
func WithStopHandler(handler ServerCommandHandler) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.StopHandler = handler
	}
}

// Connect begins a new ngrok [Session] by connecting to the ngrok service.
// Connect blocks until the session is successfully established or fails with
// an error. Customize session connection behavior with [ConnectOption]
// arguments.
func Connect(ctx context.Context, opts ...ConnectOption) (Session, error) {
	logger := log15.New()
	logger.SetHandler(log15.DiscardHandler())

	cfg := connectConfig{}
	for _, o := range opts {
		o(&cfg)
	}

	if cfg.Logger != nil {
		logger = toLog15(cfg.Logger)
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
		netDialer := &net.Dialer{}

		if cfg.ProxyURL != nil {
			proxied, err := proxy.FromURL(cfg.ProxyURL, netDialer)
			if err != nil {
				return nil, errProxyInit{cfg.ProxyURL, err}
			}
			dialer = proxied.(Dialer)
		} else {
			dialer = netDialer
		}
	}

	heartbeatConfig := muxado.NewHeartbeatConfig()
	if cfg.HeartbeatTolerance != 0 {
		heartbeatConfig.Tolerance = cfg.HeartbeatTolerance
	}
	if cfg.HeartbeatInterval != 0 {
		heartbeatConfig.Interval = cfg.HeartbeatInterval
	}

	session := new(sessionImpl)

	stateChanges := make(chan error, 32)

	callbackHandler := remoteCallbackHandler{
		Logger:         logger,
		sess:           session,
		stopHandler:    cfg.StopHandler,
		restartHandler: cfg.RestartHandler,
		updateHandler:  cfg.UpdateHandler,
	}

	rawDialer := func() (tunnel_client.RawSession, error) {
		conn, err := dialer.DialContext(ctx, "tcp", cfg.ServerAddr)
		if err != nil {
			return nil, errSessionDial{cfg.ServerAddr, err}
		}

		conn = tls.Client(conn, tlsConfig)

		sess := muxado.Client(conn, &muxado.Config{})
		return tunnel_client.NewRawSession(logger, sess, heartbeatConfig, callbackHandler), nil
	}

	empty := ""
	notImplemented := "the agent has not defined a callback for this operation"

	var remoteStopErr, remoteRestartErr, remoteUpdateErr = &notImplemented, &notImplemented, &notImplemented

	if cfg.StopHandler != nil {
		remoteStopErr = &empty
	}
	if cfg.RestartHandler != nil {
		remoteRestartErr = &empty
	}
	if cfg.UpdateHandler != nil {
		remoteUpdateErr = &empty
	}

	auth := proto.AuthExtra{
		Version:            libraryAgentVersion,
		Authtoken:          proto.ObfuscatedString(cfg.Authtoken),
		Metadata:           cfg.Metadata,
		OS:                 runtime.GOOS,
		Arch:               runtime.GOARCH,
		HeartbeatInterval:  int64(heartbeatConfig.Interval),
		HeartbeatTolerance: int64(heartbeatConfig.Tolerance),

		RestartUnsupportedError: remoteRestartErr,
		StopUnsupportedError:    remoteStopErr,
		UpdateUnsupportedError:  remoteUpdateErr,

		ClientType: proto.LibraryOfficialGo,
	}

	reconnect := func(sess tunnel_client.Session) error {
		resp, err := sess.Auth(auth)
		if err != nil {
			remote := false
			if resp.Error != "" {
				err = errors.New(resp.Error)
				remote = true
			}
			return errAuthFailed{remote, err}
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

		if cfg.HeartbeatHandler != nil {
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
						cfg.HeartbeatHandler(ctx, session, latency)
					}
				}
			}()
		}

		auth.Cookie = resp.Extra.Cookie
		return nil
	}

	sess := tunnel_client.NewReconnectingSession(logger, rawDialer, stateChanges, reconnect)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-stateChanges:
		if err != nil {
			sess.Close()
			return nil, err
		}
	}

	if cfg.ConnectHandler != nil {
		cfg.ConnectHandler(ctx, session)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case err, ok := <-stateChanges:
				if !ok {
					if cfg.DisconnectHandler != nil {
						logger.Info("no more state changes")
						cfg.DisconnectHandler(ctx, session, nil)
					}
					return
				}
				if err == nil && cfg.ConnectHandler != nil {
					cfg.ConnectHandler(ctx, session)
				}
				if err != nil && cfg.DisconnectHandler != nil {
					cfg.DisconnectHandler(ctx, session, err)
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

func (s *sessionImpl) Listen(ctx context.Context, cfg config.Tunnel) (Tunnel, error) {
	var (
		tunnel tunnel_client.Tunnel
		err    error
	)

	tunnelCfg, ok := cfg.(tunnelConfigPrivate)
	if !ok {
		return nil, errors.New("invalid tunnel config")
	}

	extra := tunnelCfg.Extra()

	if tunnelCfg.Proto() != "" {
		tunnel, err = s.inner().Listen(tunnelCfg.Proto(), tunnelCfg.Opts(), extra, tunnelCfg.ForwardsTo())
	} else {
		tunnel, err = s.inner().ListenLabel(tunnelCfg.Labels(), extra.Metadata, tunnelCfg.ForwardsTo())
	}

	if err != nil {
		return nil, errListen{err}
	}

	t := &tunnelImpl{
		Sess:   s,
		Tunnel: tunnel,
	}

	if httpServerCfg, ok := cfg.(interface {
		HTTPServer() *http.Server
	}); ok {
		if srv := httpServerCfg.HTTPServer(); srv != nil {
			go func() {
				_ = srv.Serve(t)
			}()
		}
	}

	return t, nil
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
	sess           Session
	stopHandler    ServerCommandHandler
	restartHandler ServerCommandHandler
	updateHandler  ServerCommandHandler
}

func (rc remoteCallbackHandler) OnStop(_ *proto.Stop, respond tunnel_client.HandlerRespFunc) {
	if rc.stopHandler != nil {
		resp := new(proto.StopResp)
		close := true
		if err := rc.stopHandler(context.TODO(), rc.sess); err != nil {
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
	if rc.restartHandler != nil {
		resp := new(proto.RestartResp)
		close := true
		if err := rc.restartHandler(context.TODO(), rc.sess); err != nil {
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
	if rc.updateHandler != nil {
		resp := new(proto.UpdateResp)
		if err := rc.updateHandler(context.TODO(), rc.sess); err != nil {
			resp.Error = err.Error()
		}
		if err := respond(resp); err != nil {
			rc.Warn("error responding to restart request", "error", err)
		}
	}
}
