package config

// WithMetadata sets the opaque metadata string for this tunnel.
func WithMetadata(meta string) interface {
	HTTPEndpointOption
	TCPEndpointOption
	TLSEndpointOption
	LabeledTunnelOption
} {
	return metadataOption(meta)
}

type metadataOption string

func (meta metadataOption) ApplyHTTP(cfg *httpOptions) {
	cfg.Metadata = string(meta)
}

func (meta metadataOption) ApplyTCP(cfg *tcpOptions) {
	cfg.Metadata = string(meta)
}

func (meta metadataOption) ApplyTLS(cfg *tlsOptions) {
	cfg.Metadata = string(meta)
}

func (meta metadataOption) ApplyLabeled(cfg *labeledOptions) {
	cfg.Metadata = string(meta)
}
