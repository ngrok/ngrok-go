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

// The interface implemented by an ngrok session object.
type Session interface {
	// Start a new tunnel over the ngrok session.
	StartTunnel(ctx context.Context, cfg config.Tunnel) (Tunnel, error)

	// Close the ngrok session.
	// This also closes all existing tunnels tied to the session.
	Close() error
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

type SessionConnectHandler func(ctx context.Context, sess Session)
type SessionDisconnectHandler func(ctx context.Context, sess Session, err error)
type SessionHeartbeatHandler func(ctx context.Context, sess Session, latency time.Duration)

type ServerCommandHandler func(ctx context.Context, sess Session) error

type ConnectOption func(*connectConfig)

// Options to use when establishing an ngrok session.
type connectConfig struct {
	// Your ngrok Authtoken.
	Authtoken string
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

// Use the provided opaque metadata string for this session.
// Sets the [ConnectConfig].Metadata field.
func WithMetadata(meta string) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.Metadata = meta
	}
}

// Use the provided dialer for establishing a TCP connection to the ngrok
// server.
// Sets the [ConnectConfig].Dialer field. Takes precedence over ProxyURL if both
// are specified.
func WithDialer(dialer Dialer) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.Dialer = dialer
	}
}

// Proxy requests through the server identified by the provided URL when using
// the default Dialer.
// Sets the [ConnectConfig].ProxyURL field. Ignored if a custom Dialer is in use.
func WithProxyURL(url *url.URL) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.ProxyURL = url
	}
}

// Use the provided Authtoken to authenticate this session.
// Sets the [ConnectConfig].Authtoken field.
func WithAuthtoken(token string) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.Authtoken = token
	}
}

// WithAuthtokenFromEnv populates the authtoken with one defined in the standard
// NGROK_AUTHTOKEN environment variable.
// Sets the [ConnectConfig].Authtoken field.
func WithAuthtokenFromEnv() ConnectOption {
	return WithAuthtoken(os.Getenv("NGROK_AUTHTOKEN"))
}

// Connect to the ngrok server in a specific region.
// Overwrites the [ConnectConfig].ServerAddr field.
func WithRegion(region string) ConnectOption {
	return func(cfg *connectConfig) {
		if region != "" {
			cfg.ServerAddr = fmt.Sprintf("tunnel.%s.ngrok.com:443", region)
		}
	}
}

// Connect to the provided ngrok server.
// Sets the [ConnectConfig].Server field.
func WithServer(addr string) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.ServerAddr = addr
	}
}

// Use the provided [x509.CertPool] to authenticate the ngrok server
// certificate.
// Sets the [ConnectConfig].CAPool field.
func WithCA(pool *x509.CertPool) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.CAPool = pool
	}
}

// Set the heartbeat tolerance for the session.
// If the session's heartbeats are outside of their interval by this duration,
// the server will assume the session is dead and close it.
func WithHeartbeatTolerance(tolerance time.Duration) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.HeartbeatTolerance = tolerance
	}
}

// Set the heartbeat interval for the session.
// This value determines how often we send application level
// heartbeats to the server go check connection liveness.
func WithHeartbeatInterval(interval time.Duration) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.HeartbeatInterval = interval
	}
}

// Log to a simplified logging interface.
// This is a "lowest common denominator" interface that should be simple to
// adapt other loggers to. Examples are provided in `log15adapter` and
// `pgxadapter`.
// If the provided `Logger` also implements the `log15.Logger` interface, it
// will be used directly.
func WithLogger(logger log.Logger) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.Logger = logger
	}
}

func WithConnectHandler(handler SessionConnectHandler) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.ConnectHandler = handler
	}
}
func WithDisconnectHandler(handler SessionDisconnectHandler) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.DisconnectHandler = handler
	}
}
func WithHeartbeatHandler(handler SessionHeartbeatHandler) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.HeartbeatHandler = handler
	}
}

func WithStopHandler(handler ServerCommandHandler) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.StopHandler = handler
	}
}

// Connect to the ngrok server and start a new session.
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
		Authtoken:          cfg.Authtoken,
		Metadata:           cfg.Metadata,
		OS:                 runtime.GOOS,
		Arch:               runtime.GOARCH,
		HeartbeatInterval:  int64(heartbeatConfig.Interval),
		HeartbeatTolerance: int64(heartbeatConfig.Tolerance),

		RestartUnsupportedError: remoteRestartErr,
		StopUnsupportedError:    remoteStopErr,
		UpdateUnsupportedError:  remoteUpdateErr,

		ClientType: proto.Library,
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

func (s *sessionImpl) StartTunnel(ctx context.Context, cfg config.Tunnel) (Tunnel, error) {
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
		return nil, errStartTunnel{err}
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
