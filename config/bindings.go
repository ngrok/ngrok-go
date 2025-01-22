package config

type bindings string

// WithBinding configures ingress for an endpoint
//
// The requestedBinding argument specifies the type of ingress for the endpoint.
func WithBinding(requestedBinding string) interface {
	HTTPEndpointOption
	TLSEndpointOption
	TCPEndpointOption
} {
	return bindings(requestedBinding)
}

func (b bindings) ApplyTLS(cfg *tlsOptions) {
	cfg.Binding = string(b)
}

func (b bindings) ApplyTCP(cfg *tcpOptions) {
	cfg.Binding = string(b)
}

func (b bindings) ApplyHTTP(cfg *httpOptions) {
	cfg.Binding = string(b)
}
