package ngrok

import (
	"crypto/tls"
	"net/url"
	"sync"
)

// Endpoint is the interface implemented by both
// [*EndpointListener] and [*EndpointForwarder].
type Endpoint interface {
	endpoint()
}

// baseEndpoint implements the common functionality for both EndpointListener and
// EndpointForwarder.
type baseEndpoint struct {
	agent          *Agent
	name           string
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

func (e *baseEndpoint) endpoint() {}

// Agent returns the [*Agent] that created this Endpoint.
func (e *baseEndpoint) Agent() *Agent {
	return e.agent
}

// PoolingEnabled returns whether the endpoint supports pooling set by WithPoolingEnabled.
func (e *baseEndpoint) PoolingEnabled() bool {
	return e.poolingEnabled
}

// Bindings returns the endpoint's bindings set by WithBindings
func (e *baseEndpoint) Bindings() []string {
	return e.bindings
}

// Description returns the endpoint's human-readable description set by WithDescription.
func (e *baseEndpoint) Description() string {
	return e.description
}

// Done returns a channel that is closed when the endpoint stops.
func (e *baseEndpoint) Done() <-chan struct{} {
	return e.doneChannel
}

// Wait blocks until the endpoint stops.
func (e *baseEndpoint) Wait() {
	<-e.doneChannel
}

// ID returns the unique endpoint identifier assigned by the ngrok cloud service.
func (e *baseEndpoint) ID() string {
	return e.id
}

// Metadata returns the endpoint's opaque user-defined metadata set by WithMetadata.
func (e *baseEndpoint) Metadata() string {
	return e.metadata
}

// Name returns the endpoint's human-readable name set by WithName.
func (e *baseEndpoint) Name() string {
	return e.name
}

// Protocol is sugar for URL().Scheme
func (e *baseEndpoint) Protocol() string {
	return e.endpointURL.Scheme
}

// AgentTLSTermination returns the TLS config that the agent uses to terminate TLS connections.
func (e *baseEndpoint) AgentTLSTermination() *tls.Config {
	return e.agentTLSConfig
}

// TrafficPolicy returns the traffic policy for the endpoint.
func (e *baseEndpoint) TrafficPolicy() string {
	return e.trafficPolicy
}

// URL returns the Endpoint's URL.
func (e *baseEndpoint) URL() *url.URL {
	return &e.endpointURL
}

// signalDone safely closes the done channel using sync.Once
func (e *baseEndpoint) signalDone() {
	e.doneOnce.Do(func() {
		close(e.doneChannel)
	})
}
