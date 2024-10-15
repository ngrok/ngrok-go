package config

type nameOption string

func WithName(name string) interface {
	HTTPEndpointOption
	TCPEndpointOption
	TLSEndpointOption
	LabeledTunnelOption
} {
	return nameOption(name)
}

func (opt nameOption) ApplyHTTP(opts *httpOptions) {
	opts.Name = string(opt)
}

func (opt nameOption) ApplyTLS(opts *tlsOptions) {
	opts.Name = string(opt)
}

func (opt nameOption) ApplyTCP(opts *tcpOptions) {
	opts.Name = string(opt)
}

func (opt nameOption) ApplyLabeled(opts *labeledOptions) {
	opts.Name = string(opt)
}
