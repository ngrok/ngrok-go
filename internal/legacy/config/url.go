package config

type urlOption string

func WithURL(name string) interface {
	HTTPEndpointOption
	TLSEndpointOption
	TCPEndpointOption
} {
	return urlOption(name)
}

func (opt urlOption) ApplyHTTP(opts *httpOptions) {
	opts.URL = string(opt)
}

func (opt urlOption) ApplyTLS(opts *tlsOptions) {
	opts.URL = string(opt)
}

func (opt urlOption) ApplyTCP(opts *tcpOptions) {
	opts.URL = string(opt)
}
