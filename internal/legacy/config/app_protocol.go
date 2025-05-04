package config

type appProtocol string

func (ap appProtocol) ApplyHTTP(cfg *httpOptions) {
	cfg.commonOpts.ForwardsProto = string(ap)
}

// WithAppProtocol declares the protocol that the upstream service speaks.
// This may be used by the ngrok edge to make decisions regarding protocol
// upgrades or downgrades.
//
// Currently, `http2` is the only valid string, and will cause connections
// received from HTTP endpoints to always use HTTP/2.
func WithAppProtocol(proto string) HTTPEndpointOption {
	return appProtocol(proto)
}
