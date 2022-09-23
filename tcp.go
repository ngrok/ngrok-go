package ngrok

import "github.com/ngrok/ngrok-go/internal/tunnel/proto"

// The options for a TCP edge.
type TCPConfig struct {
	// Common tunnel configuration options.
	CommonConfig *CommonConfig
	// The TCP address to request for this edge.
	RemoteAddr string
}

// Construct a new set of TCP options.
func TCPOptions() *TCPConfig {
	opts := &TCPConfig{}
	opts.CommonConfig = &CommonConfig{}
	return opts
}

// Set the TCP address to request for this edge.
func (cfg *TCPConfig) WithRemoteAddr(addr string) *TCPConfig {
	cfg.RemoteAddr = addr
	return cfg
}

// Use the provided PROXY protocol version for connections over this tunnel.
// Sets the [CommonConfig].ProxyProto field.
func (cfg *TCPConfig) WithProxyProto(version ProxyProtoVersion) *TCPConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithProxyProto(version)
	return cfg
}

// Use the provided opaque metadata string for this tunnel.
// Sets the [CommonConfig].Metadata field.
func (cfg *TCPConfig) WithMetadata(meta string) *TCPConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithMetadata(meta)
	return cfg
}

// Use the provided backend as the tunnel's ForwardsTo string.
// Sets the [CommonConfig].ForwardsTo field.
func (cfg *TCPConfig) WithForwardsTo(address string) *TCPConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithForwardsTo(address)
	return cfg
}

// Add the provided [CIDRRestriction] to the tunnel.
// Concatenates all provided Allowed and Denied lists with the existing ones.
func (cfg *TCPConfig) WithCIDRRestriction(set ...*CIDRRestriction) *TCPConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithCIDRRestriction(set...)
	return cfg
}

func (cfg *TCPConfig) toProtoConfig() *proto.TCPOptions {
	return &proto.TCPOptions{
		Addr:          cfg.RemoteAddr,
		IPRestriction: cfg.CommonConfig.CIDRRestrictions.toProtoConfig(),
		ProxyProto:    proto.ProxyProto(cfg.CommonConfig.ProxyProto),
	}
}

func (cfg *TCPConfig) applyTunnelConfig(tcfg *tunnelConfig) {
	cfg.CommonConfig.applyTunnelConfig(tcfg)

	tcfg.proto = "tcp"
	tcfg.opts = cfg.toProtoConfig()
}
