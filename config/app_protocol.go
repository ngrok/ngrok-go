package config

type appProtocol string

func (ap appProtocol) ApplyHTTP(cfg *httpOptions) {
	cfg.commonOpts.ForwardsProto = string(ap)
}

func (ap appProtocol) ApplyLabeled(cfg *labeledOptions) {
	cfg.commonOpts.ForwardsProto = string(ap)
}

func WithAppProtocol(proto string) interface {
	HTTPEndpointOption
	LabeledTunnelOption
} {
	return appProtocol(proto)
}
