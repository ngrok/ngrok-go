package ngrok

import (
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"time"

	"golang.ngrok.com/ngrok/v2/internal/legacy"
)

// AgentOption is a functional option used to configure NewAgent.
type AgentOption func(*agentOpts)

// agentOpts stores configuration for Agent.
type agentOpts struct {
	authtoken          string
	logger             *slog.Logger
	connectURL         string
	autoConnect        bool
	clientInfo         clientInfo
	dialer             Dialer
	description        string
	metadata           string
	proxyURL           string
	connectCAs         *x509.CertPool
	tlsConfig          func(*tls.Config)
	multiLeg           bool
	heartbeatInterval  time.Duration
	heartbeatTolerance time.Duration
	// Event handlers registered with the agent
	eventHandlers []EventHandler
	// RPC handler for server commands
	rpcHandler RPCHandler
	// Store ngrok SDK options
	sessionOpts []legacy.ConnectOption
}

type clientInfo struct {
	clientType string
	version    string
	comments   []string
}

// defaultAgentOpts returns the default options for an agent.
func defaultAgentOpts() *agentOpts {
	return &agentOpts{
		autoConnect: true,
		sessionOpts: []legacy.ConnectOption{},
	}
}

// WithAgentConnectCAs defines the CAs used to validate the TLS certificate
// returned by the ngrok service when establishing a session.
//
// See https://ngrok.com/docs/agent/config/v3/#connect_cas
func WithAgentConnectCAs(pool *x509.CertPool) AgentOption {
	return func(opts *agentOpts) {
		opts.connectCAs = pool
		opts.sessionOpts = append(opts.sessionOpts, legacy.WithCA(pool))
	}
}

// WithAgentConnectURL defines the URL the agent connects to in order to
// establish a connection to the ngrok cloud service.
//
// See https://ngrok.com/docs/agent/config/v3/#connect_url
func WithAgentConnectURL(addr string) AgentOption {
	return func(opts *agentOpts) {
		opts.connectURL = addr
		opts.sessionOpts = append(opts.sessionOpts, legacy.WithServer(addr))
	}
}

// WithAuthtoken specifies the authtoken to use for authenticating to the
// ngrok cloud service during Connect.
//
// See https://ngrok.com/docs/agent/#authtokens
func WithAuthtoken(token string) AgentOption {
	return func(opts *agentOpts) {
		opts.authtoken = token
		opts.sessionOpts = append(opts.sessionOpts, legacy.WithAuthtoken(token))
	}
}

// WithAutoConnect controls whether the Agent will automatically call
// Connect(). When enabled, if an endpoint is created via Listen() or Connect()
// and the Agent does not have an active session, it will automatically Connect().
func WithAutoConnect(auto bool) AgentOption {
	return func(opts *agentOpts) {
		opts.autoConnect = auto
	}
}

// WithClientInfo provides client information to the ngrok cloud service.
func WithClientInfo(clientType, version string, comments ...string) AgentOption {
	return func(opts *agentOpts) {
		opts.clientInfo = clientInfo{
			clientType: clientType,
			version:    version,
			comments:   comments,
		}
		opts.sessionOpts = append(opts.sessionOpts, legacy.WithClientInfo(clientType, version, comments...))
	}
}

// WithDialer customizes how the Agent establishes connections to the ngrok
// cloud service.
func WithDialer(dialer Dialer) AgentOption {
	return func(opts *agentOpts) {
		opts.dialer = dialer
		opts.sessionOpts = append(opts.sessionOpts, legacy.WithDialer(dialer))
	}
}

// WithAgentDescription sets a human-readable description for the agent session.
func WithAgentDescription(desc string) AgentOption {
	return func(opts *agentOpts) {
		opts.description = desc
	}
}

// WithHeartbeatInterval sets how often the agent will send heartbeat
// messages to the ngrok service.
//
// See https://ngrok.com/docs/agent/#heartbeats
func WithHeartbeatInterval(interval time.Duration) AgentOption {
	return func(opts *agentOpts) {
		opts.heartbeatInterval = interval
		opts.sessionOpts = append(opts.sessionOpts, legacy.WithHeartbeatInterval(interval))
	}
}

// WithHeartbeatTolerance sets how long to wait for a heartbeat response
// before assuming the connection is dead.
//
// See https://ngrok.com/docs/agent/#heartbeats
func WithHeartbeatTolerance(tolerance time.Duration) AgentOption {
	return func(opts *agentOpts) {
		opts.heartbeatTolerance = tolerance
		opts.sessionOpts = append(opts.sessionOpts, legacy.WithHeartbeatTolerance(tolerance))
	}
}

// WithLogger sets the logger to use for the agent.
// Accepts a standard log/slog.Logger from the Go standard library.
func WithLogger(logger *slog.Logger) AgentOption {
	return func(opts *agentOpts) {
		opts.logger = logger

		// Convert slog logger to log15 for the legacy API
		log15Logger := legacy.SlogToLog15(logger)
		opts.sessionOpts = append(opts.sessionOpts, legacy.WithLogger(log15Logger))
	}
}

// WithAgentMetadata sets opaque, machine-readable metadata for the agent session.
//
// See https://ngrok.com/docs/api/resources/tunnel-sessions/#response-1
func WithAgentMetadata(meta string) AgentOption {
	return func(opts *agentOpts) {
		opts.metadata = meta
		opts.sessionOpts = append(opts.sessionOpts, legacy.WithMetadata(meta))
	}
}

// WithMultiLeg enables connecting to the ngrok service on secondary legs. This
// option is EXPERIMENTAL and may be removed without a breaking version change.
func WithMultiLeg(enable bool) AgentOption {
	return func(opts *agentOpts) {
		opts.multiLeg = enable
		opts.sessionOpts = append(opts.sessionOpts, legacy.WithMultiLeg(enable))
	}
}

// WithProxyURL sets the proxy URL to use when connecting to the ngrok service.
// The URL will be parsed and processed during Connect.
//
// If used with WithDialer, the custom dialer will be used to establish the
// connection to the proxy, which will then connect to the ngrok service.
//
// See https://ngrok.com/docs/agent/config/v3/#proxy_url
func WithProxyURL(urlSpec string) AgentOption {
	return func(opts *agentOpts) {
		opts.proxyURL = urlSpec
	}
}

// WithTLSConfig customizes the TLS configuration for connections to the ngrok
// service.
func WithTLSConfig(tlsCustomizer func(*tls.Config)) AgentOption {
	return func(opts *agentOpts) {
		opts.tlsConfig = tlsCustomizer
		opts.sessionOpts = append(opts.sessionOpts, legacy.WithTLSConfig(tlsCustomizer))
	}
}

// WithEventHandler registers a callback to receive events from the Agent. If
// called multiple times, each handler will receive callbacks. See
// [EventHandler] for details on correctly authoring handlers.
func WithEventHandler(handler EventHandler) AgentOption {
	return func(opts *agentOpts) {
		opts.eventHandlers = append(opts.eventHandlers, handler)
	}
}

// WithRPCHandler registers a handler for RPC commands from the ngrok service.
// This handler will be called when the agent receives RPC requests like StopAgent,
// RestartAgent, or UpdateAgent.
func WithRPCHandler(handler RPCHandler) AgentOption {
	return func(opts *agentOpts) {
		opts.rpcHandler = handler
		// Note: Legacy handlers will be registered in the agent.Connect method
		// to have access to the agent's session
	}
}
