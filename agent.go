package ngrok

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"sync"
	"time"

	"golang.org/x/net/proxy"

	"golang.ngrok.com/ngrok/v2/internal/legacy"
	"golang.ngrok.com/ngrok/v2/internal/legacy/config"
	"golang.ngrok.com/ngrok/v2/rpc"
)

// Agent is the main interface for interacting with the ngrok service.
type Agent interface {
	// Connect begins a new Session by connecting and authenticating to the ngrok cloud service.
	Connect(context.Context) error

	// Disconnect terminates the current Session which disconnects it from the ngrok cloud service.
	Disconnect() error

	// Session returns an object describing the connection of the Agent to the ngrok cloud service.
	Session() (AgentSession, error)

	// Endpoints returns the list of endpoints created by this Agent from calls to either Listen or Forward.
	Endpoints() []Endpoint

	// Listen creates an Endpoint which returns received connections to the caller via an EndpointListener.
	Listen(context.Context, ...EndpointOption) (EndpointListener, error)

	// Forward creates an Endpoint which forwards received connections to a target upstream URL.
	Forward(context.Context, *Upstream, ...EndpointOption) (EndpointForwarder, error)
}

// Dialer is an interface that is satisfied by net.Dialer or you can specify your
// own implementation.
type Dialer interface {
	Dial(network, address string) (net.Conn, error)
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// agent implements the Agent interface.
type agent struct {
	mu           sync.RWMutex
	sess         legacy.Session
	agentSession *agentSession
	opts         *agentOpts
	endpoints    []Endpoint
	// Event handlers registered with this agent
	eventHandlers []EventHandler
	eventMutex    sync.RWMutex // Protects eventHandlers
}

// NewAgent creates a new Agent object.
func NewAgent(agentOpts ...AgentOption) (Agent, error) {
	opts := defaultAgentOpts()
	for _, opt := range agentOpts {
		opt(opts)
	}

	return &agent{
		opts:          opts,
		endpoints:     make([]Endpoint, 0),
		eventHandlers: opts.eventHandlers,
	}, nil
}

// Connect begins a new Session by connecting and authenticating to the ngrok
// cloud service.
func (a *agent) Connect(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// If we're already connected, return an error
	if a.sess != nil && a.agentSession != nil {
		return errors.New("agent already connected")
	}

	// Add legacy connect handlers for events
	legacyOpts := append([]legacy.ConnectOption{}, a.opts.sessionOpts...)

	// Process proxy URL if provided
	if a.opts.proxyURL != "" {
		parsedURL, err := url.Parse(a.opts.proxyURL)
		if err != nil {
			return fmt.Errorf("invalid proxy URL: %w", err)
		}

		// Determine the base dialer to use for connecting to the proxy
		baseDialer := a.opts.dialer
		if baseDialer == nil {
			// If no custom dialer is provided, use a standard net.Dialer
			baseDialer = &net.Dialer{}
		}

		// Create a proxy dialer using the base dialer
		proxyDialer, err := proxy.FromURL(parsedURL, baseDialer)
		if err != nil {
			return fmt.Errorf("failed to initialize proxy: %w", err)
		}

		// We know FromURL returns a Dialer-compatible type
		dialer, ok := proxyDialer.(Dialer)
		if !ok {
			return fmt.Errorf("proxy dialer is not compatible with ngrok Dialer interface")
		}

		// Set the dialer in our options
		a.opts.dialer = dialer
		// Pass it to the legacy package
		legacyOpts = append(legacyOpts, legacy.WithDialer(dialer))
	}

	// Hook up connect event
	legacyOpts = append(legacyOpts, legacy.WithConnectHandler(func(_ context.Context, sess legacy.Session) {
		a.emitEvent(newAgentConnectSucceeded(a, a.agentSession))
	}))

	// Hook up disconnect event
	legacyOpts = append(legacyOpts, legacy.WithDisconnectHandler(func(_ context.Context, sess legacy.Session, err error) {
		a.emitEvent(newAgentDisconnected(a, a.agentSession, err))

		if !Retryable(err) {
			sess.Close()
		}
	}))

	// Hook up heartbeat event
	legacyOpts = append(legacyOpts, legacy.WithHeartbeatHandler(func(_ context.Context, sess legacy.Session, latency time.Duration) {
		a.emitEvent(newAgentHeartbeatReceived(a, a.agentSession, latency))
	}))

	// If an RPC handler is registered, hook up the command handlers
	if a.opts.rpcHandler != nil {
		// Register the command handlers that delegate to the RPC handler
		legacyOpts = append(legacyOpts,
			legacy.WithStopHandler(a.createCommandHandler(rpc.StopAgentMethod)),
			legacy.WithRestartHandler(a.createCommandHandler(rpc.RestartAgentMethod)),
			legacy.WithUpdateHandler(a.createCommandHandler(rpc.UpdateAgentMethod)),
		)
	}

	// Create a new ngrok session
	sess, err := legacy.Connect(ctx, legacyOpts...)
	if err != nil {
		return wrapError(err)
	}

	// Create our AgentSession wrapper
	a.sess = sess
	a.agentSession = &agentSession{
		warnings:  sess.Warnings(),
		agent:     a,
		startedAt: time.Now(),
	}

	return nil
}

// Disconnect terminates the current Session which disconnects it from the ngrok
// cloud service.
func (a *agent) Disconnect() error {
	// Get what we need under lock
	a.mu.Lock()
	sess := a.sess
	endpoints := a.endpoints
	a.sess = nil
	a.agentSession = nil
	a.endpoints = make([]Endpoint, 0)
	a.mu.Unlock()

	if sess == nil {
		return nil
	}

	// Signal done for all endpoints (not holding the lock)
	for _, endpoint := range endpoints {
		// Only signal done, don't remove (already cleared the list)
		if e, ok := endpoint.(interface{ signalDone() }); ok {
			e.signalDone()
		}
	}

	// Close session (not holding the lock)
	err := sess.Close()
	return wrapError(err)
}

// Session returns an object describing the connection of the Agent to the ngrok
// cloud service.
func (a *agent) Session() (AgentSession, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.sess == nil || a.agentSession == nil {
		return nil, errors.New("agent not connected")
	}

	return a.agentSession, nil
}

// Endpoints returns the list of endpoints created by this Agent.
func (a *agent) Endpoints() []Endpoint {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Return a copy to avoid race conditions
	return slices.Clone(a.endpoints)
}

// createListener creates an endpointListener for internal use
func (a *agent) createListener(ctx context.Context, endpointOpts *endpointOpts) (*endpointListener, error) {
	// Get the session
	a.mu.RLock()
	sess := a.sess
	a.mu.RUnlock()

	// Determine URL scheme and configure endpoint
	scheme, err := determineURLScheme(endpointOpts.url)
	if err != nil {
		return nil, err
	}
	tunnelConfig, err := configureEndpoint(scheme, endpointOpts)
	if err != nil {
		return nil, err
	}

	// Create tunnel and parse URL
	tunnel, err := sess.Listen(ctx, tunnelConfig)
	if err != nil {
		return nil, wrapError(err)
	}
	tunnelURL, err := url.Parse(tunnel.URL())
	if err != nil {
		return nil, fmt.Errorf("failed to parse tunnel URL: %w", err)
	}

	// Validate upstream URL format if provided
	if endpointOpts.upstreamURL != "" {
		_, err = url.Parse(endpointOpts.upstreamURL)
		if err != nil {
			return nil, fmt.Errorf("invalid upstream URL: %w", err)
		}
	}

	// Create endpoint listener
	endpoint := &endpointListener{
		baseEndpoint: baseEndpoint{
			agent:          a,
			id:             tunnel.ID(),
			poolingEnabled: endpointOpts.poolingEnabled,
			bindings:       endpointOpts.bindings,
			description:    endpointOpts.description,
			metadata:       endpointOpts.metadata,
			agentTLSConfig: endpointOpts.agentTLSConfig,
			trafficPolicy:  endpointOpts.trafficPolicy,
			endpointURL:    *tunnelURL,
			doneChannel:    make(chan struct{}),
			doneOnce:       &sync.Once{},
		},
		tunnel: tunnel,
	}

	// Add the endpoint to our list
	a.mu.Lock()
	a.endpoints = append(a.endpoints, endpoint)
	a.mu.Unlock()

	return endpoint, nil
}

// Listen creates an EndpointListener.
func (a *agent) Listen(ctx context.Context, opts ...EndpointOption) (EndpointListener, error) {
	// Apply all options
	endpointOpts := defaultEndpointOpts()
	for _, opt := range opts {
		opt(endpointOpts)
	}

	// Ensure we're connected
	if err := a.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Create the listener using the helper method
	listener, err := a.createListener(ctx, endpointOpts)
	if err != nil {
		return nil, err
	}

	return listener, nil
}

// ensureConnected handles automatic connection and verifies connection state
func (a *agent) ensureConnected(ctx context.Context) error {
	// First check if we're already connected (with a read lock)
	a.mu.RLock()
	sessionExists := a.sess != nil
	a.mu.RUnlock()

	// Only try to connect if needed and auto-connect is enabled
	if !sessionExists && a.opts.autoConnect {
		if err := a.Connect(ctx); err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
	}

	// Final verification that we're connected
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.sess == nil {
		return errors.New("agent not connected, call Connect() first")
	}

	return nil
}

// removeEndpoint removes an endpoint from the agent's list
func (a *agent) removeEndpoint(endpoint Endpoint) {
	// Remove the endpoint from our list under lock
	a.mu.Lock()
	for i, e := range a.endpoints {
		if e == endpoint {
			a.endpoints = append(a.endpoints[:i], a.endpoints[i+1:]...)
			break
		}
	}
	a.mu.Unlock()
}

// emitEvent sends an event to all registered handlers
func (a *agent) emitEvent(evt Event) {
	a.eventMutex.RLock()
	handlers := make([]EventHandler, len(a.eventHandlers))
	copy(handlers, a.eventHandlers)
	a.eventMutex.RUnlock()

	for _, handler := range handlers {
		// Call the handler directly
		// Note: The handler is responsible for not blocking
		handler(evt)
	}
}

// createCommandHandler returns a legacy.ServerCommandHandler that delegates to the RPCHandler
// for the specified RPC method.
func (a *agent) createCommandHandler(method string) legacy.ServerCommandHandler {
	return func(ctx context.Context, sess legacy.Session) error {
		if a.opts.rpcHandler == nil {
			return nil
		}

		// Get the current agent session
		agentSession, err := a.Session()
		if err != nil {
			return err
		}

		// Create request object with the specified method
		req := &rpcRequest{
			method:  method,
			payload: nil, // No payload for now
		}

		// Call the RPC handler
		_, err = a.opts.rpcHandler(ctx, agentSession, req)
		// Ignore response payload for now
		return err
	}
}

// Forward creates an EndpointForwarder that forwards traffic to the specified upstream.
// The upstream parameter is required and must be created using WithUpstream().
// Additional endpoint options can be provided to configure the endpoint.
func (a *agent) Forward(ctx context.Context, upstream *Upstream, opts ...EndpointOption) (EndpointForwarder, error) {
	// Apply all base options first
	endpointOpts := defaultEndpointOpts()

	// Set upstream values directly from the Upstream object
	endpointOpts.upstreamURL = upstream.addr
	endpointOpts.upstreamProtocol = upstream.protocol
	endpointOpts.upstreamTLSClientConfig = upstream.tlsClientConfig

	// Convert the proxy protocol to config.ProxyProtoVersion
	if upstream.proxyProto != "" {
		var proxyVersion config.ProxyProtoVersion
		switch upstream.proxyProto {
		case ProxyProtoV1:
			proxyVersion = config.ProxyProtoV1
		case ProxyProtoV2:
			proxyVersion = config.ProxyProtoV2
		default:
			return nil, fmt.Errorf("unsupported proxy protocol: %s", upstream.proxyProto)
		}
		endpointOpts.proxyProtoVersion = proxyVersion
	}

	// Apply additional options
	for _, opt := range opts {
		opt(endpointOpts)
	}

	// Ensure we're connected
	if err := a.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Create the listener using the helper method
	listener, err := a.createListener(ctx, endpointOpts)
	if err != nil {
		return nil, err
	}

	// Parse upstream URL - we know it exists and is valid from createListener
	upstreamURL, _ := url.Parse(endpointOpts.upstreamURL)

	// Create the forwarder
	endpoint := &endpointForwarder{
		baseEndpoint:            listener.baseEndpoint, // reuse the baseEndpoint from listener
		listener:                listener,
		upstreamURL:             *upstreamURL,
		upstreamProtocol:        endpointOpts.upstreamProtocol,
		upstreamTLSClientConfig: endpointOpts.upstreamTLSClientConfig,
		proxyProtocol:           upstream.proxyProto,
		upstreamDialer:          upstream.dialer,
	}

	// Start the forwarding process
	endpoint.start(ctx)

	// Add the endpoint to our list
	a.mu.Lock()
	a.endpoints = append(a.endpoints, endpoint)
	a.mu.Unlock()

	return endpoint, nil
}
