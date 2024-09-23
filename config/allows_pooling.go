package config

type allowsPoolingOption bool

func WithAllowsPooling(allowsPooling bool) interface {
	HTTPEndpointOption
	TCPEndpointOption
	TLSEndpointOption
} {
	return allowsPoolingOption(allowsPooling)
}

func (opt allowsPoolingOption) ApplyHTTP(opts *httpOptions) {
	opts.AllowsPooling = bool(opt)
}

func (opt allowsPoolingOption) ApplyTLS(opts *tlsOptions) {
	opts.AllowsPooling = bool(opt)
}

func (opt allowsPoolingOption) ApplyTCP(opts *tcpOptions) {
	opts.AllowsPooling = bool(opt)
}
