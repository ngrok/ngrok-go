package config

type descriptionOption string

func WithDescription(name string) interface {
	HTTPEndpointOption
	TCPEndpointOption
	TLSEndpointOption
} {
	return descriptionOption(name)
}

func (opt descriptionOption) ApplyHTTP(opts *httpOptions) {
	opts.Description = string(opt)
}

func (opt descriptionOption) ApplyTLS(opts *tlsOptions) {
	opts.Description = string(opt)
}

func (opt descriptionOption) ApplyTCP(opts *tcpOptions) {
	opts.Description = string(opt)
}
