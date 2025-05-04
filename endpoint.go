package ngrok

import (
	"context"
	"crypto/tls"
	"net/url"
	"sync"
)

// Endpoint is the interface implemented by both EndpointListener and
// EndpointForwarder.
type Endpoint interface {
	// Agent returns the Agent that created this Endpoint.
	Agent() Agent

	// PoolingEnabled returns whether the endpoint supports pooling set by WithPoolingEnabled.
	PoolingEnabled() bool

	// Bindings returns the endpoint's bindings set by WithBindings
	Bindings() []string

	// Close() is equivalent to for CloseWithContext(context.Background())
	Close() error

	// CloseWithContext closes the endpoint with the provided context.
	CloseWithContext(context.Context) error

	// Description returns the endpoint's human-readable description set by WithDescription.
	Description() string

	// Done returns a channel that is closed when the endpoint stops.
	Done() <-chan struct{}

	// ID returns the unique endpoint identifier assigned by the ngrok cloud service.
	ID() string

	// Metadata returns the endpoint's opaque user-defined metadata set by WithMetadata.
	Metadata() string

	// Protocol is sugar for URL().Scheme
	Protocol() string

	// AgentTLSTermination returns the TLS config that the agent uses to terminate TLS connections.
	AgentTLSTermination() *tls.Config

	// TrafficPolicy returns the traffic policy for the endpoint.
	TrafficPolicy() string

	// URL returns the Endpoint's URL
	URL() *url.URL
}

// baseEndpoint implements the common functionality for both EndpointListener and
// EndpointForwarder.
type baseEndpoint struct {
	agent          Agent
	poolingEnabled bool
	bindings       []string
	description    string
	id             string
	metadata       string
	agentTLSConfig *tls.Config // TLS config for termination
	trafficPolicy  string
	endpointURL    url.URL
	doneChannel    chan struct{}
	doneOnce       *sync.Once
}

func (e *baseEndpoint) Agent() Agent {
	return e.agent
}

func (e *baseEndpoint) PoolingEnabled() bool {
	return e.poolingEnabled
}

func (e *baseEndpoint) Bindings() []string {
	return e.bindings
}

func (e *baseEndpoint) Description() string {
	return e.description
}

func (e *baseEndpoint) Done() <-chan struct{} {
	return e.doneChannel
}

func (e *baseEndpoint) ID() string {
	return e.id
}

func (e *baseEndpoint) Metadata() string {
	return e.metadata
}

func (e *baseEndpoint) Protocol() string {
	return e.endpointURL.Scheme
}

func (e *baseEndpoint) AgentTLSTermination() *tls.Config {
	return e.agentTLSConfig
}

func (e *baseEndpoint) TrafficPolicy() string {
	return e.trafficPolicy
}

func (e *baseEndpoint) URL() *url.URL {
	return &e.endpointURL
}

// signalDone safely closes the done channel using sync.Once
func (e *baseEndpoint) signalDone() {
	e.doneOnce.Do(func() {
		close(e.doneChannel)
	})
}
