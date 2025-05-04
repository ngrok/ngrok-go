package config

// ProxyProtoVersion is a valid PROXY protocol version
type ProxyProtoVersion int32

const (
	// PROXY protocol disabled
	ProxyProtoNone = ProxyProtoVersion(0)
	// PROXY protocol v1
	ProxyProtoV1 = ProxyProtoVersion(1)
	// PROXY protocol v2
	ProxyProtoV2 = ProxyProtoVersion(2)
)

type proxyProtoConfig ProxyProtoVersion

// WithProxyProto sets the PROXY protocol version for connections over this
// tunnel.
func WithProxyProto(version ProxyProtoVersion) interface {
	HTTPEndpointOption
	TCPEndpointOption
	TLSEndpointOption
} {
	return proxyProtoConfig(version)
}

func (p proxyProtoConfig) ApplyHTTP(cfg *httpOptions) {
	cfg.ProxyProto = ProxyProtoVersion(p)

}

func (p proxyProtoConfig) ApplyTCP(cfg *tcpOptions) {
	cfg.ProxyProto = ProxyProtoVersion(p)
}

func (p proxyProtoConfig) ApplyTLS(cfg *tlsOptions) {
	cfg.ProxyProto = ProxyProtoVersion(p)
}
