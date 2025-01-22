package config

type binding string

// WithBinding configures ingress for an endpoint
//
// The requestedBinding argument specifies the type of ingress for the endpoint.
func WithBinding(requestedBinding string) interface {
	HTTPEndpointOption
	TLSEndpointOption
	TCPEndpointOption
} {
	return binding(requestedBinding)
}

func (b binding) ApplyTLS(cfg *tlsOptions)   { cfg.Binding = string(b) }
func (b binding) ApplyTCP(cfg *tcpOptions)   { cfg.Binding = string(b) }
func (b binding) ApplyHTTP(cfg *httpOptions) { cfg.Binding = string(b) }
