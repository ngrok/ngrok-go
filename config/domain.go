package config

type domainOption string

// WithDomain sets the fully-qualified domain name for this edge.
//
// https://ngrok.com/docs/network-edge/domains-and-tcp-addresses/#domains
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

type hostnameOption string

// WithHostname sets the hostname for this edge.
//
// Deprecated: use WithDomain instead
func WithHostname(name string) interface {
	HTTPEndpointOption
	TLSEndpointOption
} {
	return hostnameOption(name)
}

func (opt hostnameOption) ApplyHTTP(opts *httpOptions) {
	opts.Hostname = string(opt)
}

func (opt hostnameOption) ApplyTLS(opts *tlsOptions) {
	opts.Hostname = string(opt)
}

type subdomainOption string

// WithSubdomain sets the subdomain for this edge.
//
// Deprecated: use WithDomain instead
func WithSubdomain(name string) interface {
	HTTPEndpointOption
	TLSEndpointOption
} {
	return subdomainOption(name)
}

func (opt subdomainOption) ApplyHTTP(opts *httpOptions) {
	opts.Subdomain = string(opt)
}

func (opt subdomainOption) ApplyTLS(opts *tlsOptions) {
	opts.Subdomain = string(opt)
}
