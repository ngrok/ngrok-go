package config

type poolingEnabledOption bool

func WithPoolingEnabled(poolingEnabled bool) interface {
	HTTPEndpointOption
	TCPEndpointOption
	TLSEndpointOption
} {
	return poolingEnabledOption(poolingEnabled)
}

func (opt poolingEnabledOption) ApplyHTTP(opts *httpOptions) {
	opts.PoolingEnabled = bool(opt)
}

func (opt poolingEnabledOption) ApplyTLS(opts *tlsOptions) {
	opts.PoolingEnabled = bool(opt)
}

func (opt poolingEnabledOption) ApplyTCP(opts *tcpOptions) {
	opts.PoolingEnabled = bool(opt)
}
