package ngrok

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed" // nolint
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/inconshreveable/log15/v3"
	"go.uber.org/multierr"
	"golang.org/x/net/proxy"
	"golang.org/x/sync/errgroup"

	"golang.ngrok.com/ngrok/config"

	"golang.ngrok.com/muxado/v2"
	tunnel_client "golang.ngrok.com/ngrok/internal/tunnel/client"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
	"golang.ngrok.com/ngrok/log"
)

// The ngrok library version.
//
//go:embed VERSION
var libraryAgentVersion string

// AgentVersionDeprecated is a type wrapper for [proto.AgentVersionDeprecated]
type AgentVersionDeprecated proto.AgentVersionDeprecated

func (avd *AgentVersionDeprecated) Error() string {
	return (*proto.AgentVersionDeprecated)(avd).Error()
}

// Session encapsulates an established session with the ngrok service. Sessions
// recover from network failures by automatically reconnecting.
type Session interface {
	// Listen creates a new Tunnel which will listen for new inbound
	// connections. The returned Tunnel object is a net.Listener.
	Listen(ctx context.Context, cfg config.Tunnel) (Tunnel, error)

	// Warnings returns a list of warnings generated for the session on connect/auth
	Warnings() []error

	// ListenAndForward creates a new Tunnel which will listen for new inbound
	// connections. Connections on this tunnel are automatically forwarded to
	// the provided URL.
	ListenAndForward(ctx context.Context, backend *url.URL, cfg config.Tunnel) (Forwarder, error)

	// ListenAndServeHTTP creates a new Tunnel to serve as a backend for an HTTP server. Connections will be
	// forwarded to the provided HTTP server.
	ListenAndServeHTTP(ctx context.Context, cfg config.Tunnel, server *http.Server) (Forwarder, error)

	// ListenAndHandleHTTP creates a new Tunnel to serve as a backend for an HTTP handler. Connections will be
	// forwarded to a new HTTP server and handled by the provided HTTP handler.
	ListenAndHandleHTTP(ctx context.Context, cfg config.Tunnel, handler *http.Handler) (Forwarder, error)

	// Close ends the ngrok session. All Tunnel objects created by Listen
	// on this session will be closed.
	Close() error
}

//go:embed assets/ngrok.ca.crt
var defaultCACert []byte

const defaultServer = "connect.ngrok-agent.com:443"

var leastLatencyServer = regexp.MustCompile(`^connect\.([a-z]+?-)?ngrok-agent\.com(\.lan)?:443`)

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

// SessionHeartbeatHandler is the callback type for [WithHearbeatHandler]
type SessionHeartbeatHandler func(ctx context.Context, sess Session, latency time.Duration)

// ServerCommandHandler is the callback type for [WithStopHandler]
type ServerCommandHandler func(ctx context.Context, sess Session) error

// ConnectOption is passed to [Connect] to customize session connection and establishment.
type ConnectOption func(*connectConfig)

type clientInfo struct {
	Type     string
	Version  string
	Comments []string
}

var bannedUAchar = regexp.MustCompile("[^!#$%&'*+-.^_`|~0-9a-zA-Z]")

// Formats client info as a well-formed user agent string
func (c *clientInfo) ToUserAgent() string {
	comment := ""
	if len(c.Comments) > 0 {
		comment = fmt.Sprintf(" (%s)", strings.Join(c.Comments, "; "))
	}
	return sanitizeUserAgentString(c.Type) + "/" + sanitizeUserAgentString(c.Version) + comment
}

func sanitizeUserAgentString(s string) string {
	s = strings.ReplaceAll(s, "/", "-")
	s = bannedUAchar.ReplaceAllString(s, "#")
	return s
}

// version, type, user-agent
func generateUserAgent(cs []clientInfo) string {
	var uas []string

	for _, c := range cs {
		uas = append(uas, c.ToUserAgent())
	}

	return strings.Join(uas, " ")
}

// Options to use when establishing the ngrok session.
type connectConfig struct {
	// Your ngrok Authtoken.
	Authtoken proto.ObfuscatedString
	// The address of the ngrok server to connect to.
	// Defaults to `connect.ngrok-agent.com:443`
	ServerAddr string
	// The optional addresses of the additional ngrok servers to connect to.
	AdditionalServerAddrs []string
	// Enable using multiple session legs
	EnableMultiLeg bool
	// The [tls.Config] used when connecting to the ngrok server
	TLSConfigCustomizer func(*tls.Config)
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

	// Child client types and versions used to identify specific applications
	// using this library to the ngrok service.
	ClientInfo []clientInfo

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

	remoteStopErr    *string
	remoteRestartErr *string
	remoteUpdateErr  *string

	// The logger for the session to use.
	Logger log.Logger
}

// WithMetadata configures the opaque, machine-readable metadata string for this
// session. Metadata is made available to you in the ngrok dashboard and the
// Agents API resource. It is a useful way to allow you to uniquely identify
// sessions. We suggest encoding the value in a structured format like JSON.
//
// See the [metadata parameter in the ngrok docs] for additional details.
//
// [metadata parameter in the ngrok docs]: https://ngrok.com/docs/ngrok-agent/config#metadata
func WithMetadata(meta string) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.Metadata = meta
	}
}

// WithClientInfo configures client type and version information for applications
// built on this library. This is a way for consumers of this library to identify
// themselves to the ngrok service.
//
// This will add a new entry to the `User-Agent` field in the "most significant"
// (first) position.
func WithClientInfo(clientType, version string, comments ...string) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.ClientInfo = append([]clientInfo{{clientType, version, comments}}, cfg.ClientInfo...)
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
// See the [proxy url parameter in the ngrok docs] for additional details.
//
// [proxy url parameter in the ngrok docs]: https://ngrok.com/docs/ngrok-agent/config#proxy_url
func WithProxyURL(url *url.URL) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.ProxyURL = url
	}
}

// WithAuthtoken configures the session to authenticate with the provided
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
// [full list of ngrok regions]: https://ngrok.com/docs/network-edge/#points-of-presence
// [region parameter in the ngrok docs]: https://ngrok.com/docs/ngrok-agent/config#region
func WithRegion(region string) ConnectOption {
	return func(cfg *connectConfig) {
		if region != "" {
			cfg.ServerAddr = fmt.Sprintf("connect.%s.ngrok-agent.com:443", region)
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

// WithAdditionalServers configures the network address to dial to connect to the ngrok
// service on secondary legs. Use this option only if you are connecting to a custom agent
// ingress, and have enabled multi leg.
//
// See the [server_addr parameter in the ngrok docs] for additional details.
//
// [server_addr parameter in the ngrok docs]: https://ngrok.com/docs/ngrok-agent/config#server_addr
func WithAdditionalServers(addrs []string) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.AdditionalServerAddrs = addrs
	}
}

// WithMultiLeg as true allows connecting to the ngrok service on secondary legs.
//
// See [WithAdditionalServers] if connecting to a custom agent ingress.
func WithMultiLeg(enable bool) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.EnableMultiLeg = enable
	}
}

// WithTLSConfig allows customization of the TLS connection made from the agent
// to the ngrok service. Customization is applied after the [WithServer] and
// [WithCA] options are applied.
func WithTLSConfig(tlsCustomizer func(*tls.Config)) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.TLSConfigCustomizer = tlsCustomizer
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

// WithLogger configures a logger to receive log messages from the [Session]. The
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
// receive events when the [Session] successfully connects or reconnects after
// a disconnection due to network failure.
func WithConnectHandler(handler SessionConnectHandler) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.ConnectHandler = handler
	}
}

// WithDisconnectHandler configures a function which is called each time the
// ngrok [Session] disconnects from the ngrok service. Use this option to detect
// when the ngrok session has gone temporarily offline.
//
// This handler will be called every time the [Session] encounters an error during
// or after connection. It may be called multiple times in a row; it may be
// called before any Connect handler is called and before [Connect] returns.
//
// If this function is called with a nil error, the [Session] has stopped and will
// not reconnect, usually due to [Session.Close] being called.
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
// Do not block inside this callback. It will cause the Dashboard or API Stop
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

// WithRestartHandler configures a function which is called when the ngrok service
// requests that this [Session] restarts. Your application may choose to interpret
// this callback as a request to reconnect the [Session] or restart the entire process.
//
// Errors returned by this function will be visible to the ngrok dashboard or
// API as the response to the Restart operation.
//
// Do not block inside this callback. It will cause the Dashboard or API Restart
// operation to hang. Do not call [Session].Close or [os.Exit] inside this
// callback, it will also cause the operation to hang.
//
// Instead, either spawn a goroutine to asynchronously restart, or return an error.
func WithRestartHandler(handler ServerCommandHandler) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.RestartHandler = handler
	}
}

// WithUpdateHandler configures a function which is called when the ngrok service
// requests that the application running this [Session] updates. Your application
// may use this callback to trigger a check for a newer version followed by an update
// and restart if one exists.
//
// Errors returned by this function will be visible to the ngrok dashboard or
// API as the response to the Update operation.
//
// Do not block inside this callback. It will cause the Dashboard or API Update
// operation to hang. Do not call [Session].Close or [os.Exit] inside this
// callback, it will also cause the operation to hang.
//
// Instead, spawn a goroutine to asynchronously handle the update process
// or return an error if there is no newer version to update to.
func WithUpdateHandler(handler ServerCommandHandler) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.UpdateHandler = handler
	}
}

// WithStopCommandDisabled specifies a user-friendly error message to be reported
// by the ngrok dashboard or API when a user attempts to issue a Stop command for
// this [Session].
//
// Set this error only if you wish to provide a more detailed reason for entirely
// disabling the Stop command for your application. If you wish to report an error
// while attempting to handle a Stop command, instead return that error from the
// handler function set by [WithStopHandler].
func WithStopCommandDisabled(err string) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.remoteStopErr = &err
	}
}

// WithRestartCommandDisabled specifies a user-friendly error message to be reported
// by the ngrok dashboard or API when a user attempts to issue a Restart command for
// this [Session].
//
// Set this error only if you wish to provide a more detailed reason for entirely
// disabling the Restart command for your application. If you wish to report an error
// while attempting to handle a Restart command, instead return that error from the
// handler function set by [WithRestartHandler].
func WithRestartCommandDisabled(err string) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.remoteRestartErr = &err
	}
}

// WithUpdateCommandDisabled specifies a user-friendly error message to be reported
// by the ngrok dashboard or API when a user attempts to issue a Update command for
// this [Session].
//
// Set this error only if you wish to provide a more detailed reason for entirely
// disabling the Update command for your application. If you wish to report an error
// while attempting to handle a Update command, instead return that error from the
// handler function set by [WithUpdateHandler].
func WithUpdateCommandDisabled(err string) ConnectOption {
	return func(cfg *connectConfig) {
		cfg.remoteUpdateErr = &err
	}
}

// Connect begins a new ngrok [Session] by connecting to the ngrok service,
// retrying transient failures if they occur.
//
// Connect blocks until the session is successfully established or fails with
// an error that will not be retried. Customize session connection behavior
// with [ConnectOption] arguments.
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

	rawDialer := func(legNumber uint32) (tunnel_client.RawSession, error) {
		serverAddr := cfg.ServerAddr
		if legNumber > 0 && len(cfg.AdditionalServerAddrs) >= int(legNumber) {
			serverAddr = cfg.AdditionalServerAddrs[legNumber-1]
		}
		tlsConfig := &tls.Config{
			RootCAs:    cfg.CAPool,
			ServerName: strings.Split(serverAddr, ":")[0],
			MinVersion: tls.VersionTLS12,
		}
		if cfg.TLSConfigCustomizer != nil {
			cfg.TLSConfigCustomizer(tlsConfig)
		}

		conn, err := dialer.DialContext(ctx, "tcp", serverAddr)
		if err != nil {
			return nil, errSessionDial{serverAddr, err}
		}

		conn = tls.Client(conn, tlsConfig)

		sess := muxado.Client(conn, &muxado.Config{})
		return tunnel_client.NewRawSession(logger, sess, heartbeatConfig, callbackHandler), nil
	}

	empty := ""
	notImplemented := "the agent has not defined a callback for this operation"

	if cfg.StopHandler != nil {
		cfg.remoteStopErr = &empty
	}
	if cfg.RestartHandler != nil {
		cfg.remoteRestartErr = &empty
	}
	if cfg.UpdateHandler != nil {
		cfg.remoteUpdateErr = &empty
	}

	if cfg.remoteStopErr == nil {
		cfg.remoteStopErr = &notImplemented
	}
	if cfg.remoteRestartErr == nil {
		cfg.remoteRestartErr = &notImplemented
	}
	if cfg.remoteUpdateErr == nil {
		cfg.remoteUpdateErr = &notImplemented
	}

	cfg.ClientInfo = append(
		cfg.ClientInfo,
		clientInfo{Type: string(proto.LibraryOfficialGo), Version: strings.TrimSpace(libraryAgentVersion)},
	)

	userAgent := generateUserAgent(cfg.ClientInfo)

	auth := proto.AuthExtra{
		Version:            cfg.ClientInfo[0].Version,
		ClientType:         proto.ClientType(cfg.ClientInfo[0].Type),
		UserAgent:          userAgent,
		Authtoken:          proto.ObfuscatedString(cfg.Authtoken),
		Metadata:           cfg.Metadata,
		OS:                 runtime.GOOS,
		Arch:               runtime.GOARCH,
		HeartbeatInterval:  int64(heartbeatConfig.Interval),
		HeartbeatTolerance: int64(heartbeatConfig.Tolerance),

		RestartUnsupportedError: cfg.remoteRestartErr,
		StopUnsupportedError:    cfg.remoteStopErr,
		UpdateUnsupportedError:  cfg.remoteUpdateErr,
	}

	reconnect := func(sess tunnel_client.Session, raw tunnel_client.RawSession, legNumber uint32) (int, error) {
		auth.LegNumber = legNumber
		resp, err := sess.Auth(auth)
		if err != nil {
			remote := false
			if resp.Error != "" {
				remote = true
			}
			return 0, errAuthFailed{remote, err}
		}

		if resp.Extra.DeprecationWarning != nil {
			warning := resp.Extra.DeprecationWarning
			vars := make([]any, 0, 3)
			if warning.NextMin != "" {
				vars = append(vars, "min_version", warning.NextMin)
			}
			if !warning.NextDate.IsZero() {
				vars = append(vars, "deadline", warning.NextDate)
			}
			if warning.Msg != "" {
				vars = append(vars, "extra", warning.Msg)
			}
			logger.Warn(warning.Error(), vars...)
		}

		sessionInner := &sessionInner{
			Session:            sess,
			Region:             resp.Extra.Region,
			ProtoVersion:       resp.Version,
			ServerVersion:      resp.Extra.Version,
			ClientID:           resp.Extra.Region,
			AccountName:        resp.Extra.AccountName,
			PlanName:           resp.Extra.PlanName,
			Banner:             resp.Extra.Banner,
			SessionDuration:    resp.Extra.SessionDuration,
			DeprecationWarning: resp.Extra.DeprecationWarning,
			ConnectAddresses:   resp.Extra.ConnectAddresses,
			Logger:             logger,
		}

		if legNumber == 0 {
			session.setInner(sessionInner)
		}

		if cfg.HeartbeatHandler != nil {
			// plumb a session with the proper region to the heartbeatHandler
			heartbeatSession := new(sessionImpl)
			heartbeatSession.setInner(sessionInner)
			go func() {
				// use the raw latency channel in case this is a multi-leg session
				beats := raw.Latency()
				for {
					select {
					case <-ctx.Done():
						return
					case latency, ok := <-beats:
						if !ok {
							return
						}
						cfg.HeartbeatHandler(ctx, heartbeatSession, latency)
					}
				}
			}()
		}

		auth.Cookie = resp.Extra.Cookie

		// store any connect server addresses for use in subsequent legs
		if cfg.EnableMultiLeg && legNumber == 0 && len(resp.Extra.ConnectAddresses) > 1 {
			overrideAdditionalServers := len(cfg.AdditionalServerAddrs) == 0
			for i, ca := range resp.Extra.ConnectAddresses {
				if i == 0 {
					if leastLatencyServer.MatchString(cfg.ServerAddr) {
						// lock in the leg 0 region
						logger.Debug("first leg using region", "region", resp.Extra.Region, "server", ca.ServerAddr)
						cfg.ServerAddr = ca.ServerAddr
					}
				} else if overrideAdditionalServers {
					cfg.AdditionalServerAddrs = append(cfg.AdditionalServerAddrs, ca.ServerAddr)
				}
			}
		}

		// if we are using multi-leg, we need to know how many legs to connect
		desiredLegs := 1
		if cfg.EnableMultiLeg {
			desiredLegs = 1 + len(cfg.AdditionalServerAddrs)
		}
		return desiredLegs, nil
	}

	sess := tunnel_client.NewReconnectingSession(logger, rawDialer, stateChanges, reconnect)
	// allow consumers to .Close() the session before a successful connect
	session.setInner(&sessionInner{
		Session: sess,
	})

	// performs one "pump" of the session update channel
	// returns true if there are more updates to handle
	runSessionHandlers := func() (bool, error) {
		select {
		case <-ctx.Done():
			if cfg.DisconnectHandler != nil {
				cfg.DisconnectHandler(ctx, session, ctx.Err())
				logger.Info("no more state changes")
				cfg.DisconnectHandler(ctx, session, nil)
			}
			sess.Close()
			return false, ctx.Err()
		case err, ok := <-stateChanges:
			switch {
			case !ok: // session has given up on reconnecting
				if cfg.DisconnectHandler != nil {
					logger.Info("no more state changes")
					cfg.DisconnectHandler(ctx, session, nil)
				}
				sess.Close()
				return false, nil
			case err != nil: // session encountered an error
				if cfg.DisconnectHandler != nil {
					cfg.DisconnectHandler(ctx, session, err)
				}
				return true, err
			case err == nil: // session connected successfully
				if cfg.ConnectHandler != nil {
					cfg.ConnectHandler(ctx, session)
				}
				return true, nil
			}
		}

		panic("inexhaustive case match when handling session state change")
	}

	var errs error
	for again := true; again; {
		var err error
		again, err = runSessionHandlers()
		switch {
		case again && err == nil: // successfully connected, move to goroutine and return
			again = false
		case again && err != nil: // error on reconnect
			errs = multierr.Append(errs, err)
		case !again: // gave up trying to reconnect
			errs = multierr.Append(errs, err)
			return nil, errs
		}
	}

	go func() {
		for again := true; again; again, _ = runSessionHandlers() {
		}
	}()

	return session, nil
}

type sessionImpl struct {
	raw atomic.Pointer[sessionInner]
}

type sessionInner struct {
	tunnel_client.Session

	Region             string
	ProtoVersion       string
	ServerVersion      string
	ClientID           string
	AccountName        string
	PlanName           string
	Banner             string
	SessionDuration    int64
	DeprecationWarning *proto.AgentVersionDeprecated
	ConnectAddresses   []proto.ConnectAddress

	Logger log15.Logger
}

func (s *sessionImpl) inner() *sessionInner {
	return s.raw.Load()
}

func (s *sessionImpl) setInner(raw *sessionInner) {
	s.raw.Store(raw)
}

func (s *sessionImpl) closeTunnel(clientID string, err error) error {
	return s.inner().CloseTunnel(clientID, err)
}

func (s *sessionImpl) Close() error {
	return s.inner().Close()
}

func (s *sessionImpl) Warnings() []error {
	deprecated := s.inner().DeprecationWarning
	if deprecated != nil {
		return []error{(*AgentVersionDeprecated)(deprecated)}
	}
	return nil
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
		tunnel, err = s.inner().Listen(tunnelCfg.Proto(), tunnelCfg.Opts(), extra, tunnelCfg.ForwardsTo(), tunnelCfg.ForwardsProto())
	} else {
		tunnel, err = s.inner().ListenLabel(tunnelCfg.Labels(), extra.Metadata, tunnelCfg.ForwardsTo(), tunnelCfg.ForwardsProto())
	}

	impl := &tunnelImpl{
		Sess:   s,
		Tunnel: tunnel,
	}

	// Legacy support for passing HTTP server via config options.
	// TODO: Remove this after we feel HTTP options via config have been deprecated.
	if serverCfg, ok := cfg.(interface{ HTTPServer() *http.Server }); ok {
		server := serverCfg.HTTPServer()
		if server != nil {
			go func() { _ = server.Serve(impl) }()
			impl.server = server
		}
	}

	if err == nil {
		return impl, nil
	}
	return nil, errListen{err}
}

func (s *sessionImpl) ListenAndForward(ctx context.Context, url *url.URL, cfg config.Tunnel) (Forwarder, error) {
	tunnelCfg, ok := cfg.(tunnelConfigPrivate)
	if !ok {
		return nil, errors.New("invalid tunnel config")
	}

	// Set 'Forwards To'
	tunnelCfg.WithForwardsTo(url)

	tun, err := s.Listen(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return forwardTunnel(ctx, tun, url), nil
}

func (s *sessionImpl) ListenAndServeHTTP(ctx context.Context, cfg config.Tunnel, server *http.Server) (Forwarder, error) {
	tun, err := s.Listen(ctx, cfg)
	if err != nil {
		return nil, err
	}

	mainGroup, _ := errgroup.WithContext(ctx)
	if server != nil {
		// Store server ref to close when tunnel closes
		impl, _ := tun.(*tunnelImpl)

		// Check if tunnel is already serving an HTTP server
		// TODO: Remove this once we feel HTTP options via config have been deprecated.
		if impl.server == nil {
			mainGroup.Go(func() error { return server.Serve(tun) })
			impl.server = server
		} else {
			// Inform end user that they're using a deprecated option.
			s.inner().Logger.Warn("Tunnel is serving an HTTP server via HTTP options. This has been deprecated. Please use Session.ListenAndServeHTTP instead.")
		}
	}

	return &forwarder{
		Tunnel:    tun,
		mainGroup: mainGroup,
	}, nil
}

func (s *sessionImpl) ListenAndHandleHTTP(ctx context.Context, cfg config.Tunnel, handler *http.Handler) (Forwarder, error) {
	return s.ListenAndServeHTTP(ctx, cfg, &http.Server{Handler: *handler})
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
func (s *sessionImpl) ConnectAddresses() []struct{ Region, ServerAddr string } {
	connectAddresses := make([]struct{ Region, ServerAddr string }, len(s.inner().ConnectAddresses))
	for i, addr := range s.inner().ConnectAddresses {
		connectAddresses[i] = struct{ Region, ServerAddr string }{addr.Region, addr.ServerAddr}
	}
	return connectAddresses
}

type remoteCallbackHandler struct {
	log15.Logger
	sess           *sessionImpl
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

func (rc remoteCallbackHandler) OnStopTunnel(stopTunnel *proto.StopTunnel, respond tunnel_client.HandlerRespFunc) {
	ngrokErr := &ngrokError{Message: stopTunnel.Message, ErrCode: stopTunnel.ErrorCode}
	// close the tunnel and maintain the session
	err := rc.sess.closeTunnel(stopTunnel.ClientID, ngrokErr)
	if err != nil {
		rc.Warn("error closing tunnel", "error", err)
	}
}
