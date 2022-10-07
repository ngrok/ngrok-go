package config

type domainOption string

// WithDomain sets the domain for this edge.
func WithDomain(name string) interface {
	HTTPEndpointOption
	TLSEndpointOption
} {
	return domainOption(name)
}

func (opt domainOption) ApplyHTTP(opts *httpOptions) {
	opts.Domain = string(opt)
}

func (opt domainOption) ApplyTLS(opts *tlsOptions) {
	opts.Domain = string(opt)
}
