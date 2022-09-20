package libngrok

import "github.com/ngrok/libngrok-go/internal/tunnel/proto"

type TCPConfig struct {
	CommonConfig CommonConfig
	RemoteAddr   string
}

func TCPOptions() TCPConfig {
	opts := TCPConfig{}
	return opts
}

func (cfg TCPConfig) WithRemoteAddr(addr string) TCPConfig {
	cfg.RemoteAddr = addr
	return cfg
}

func (cfg TCPConfig) WithProxyProto(version ProxyProtoVersion) TCPConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithProxyProto(version)
	return cfg
}

func (cfg TCPConfig) WithMetadata(meta string) TCPConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithMetadata(meta)
	return cfg
}

func (cfg TCPConfig) WithForwardsTo(address string) TCPConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithForwardsTo(address)
	return cfg
}

func (cfg TCPConfig) WithCIDRRestriction(set ...CIDRRestriction) TCPConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithCIDRRestriction(set...)
	return cfg
}

func (cfg TCPConfig) toProtoConfig() *proto.TCPOptions {
	return &proto.TCPOptions{
		Addr:          cfg.RemoteAddr,
		IPRestriction: cfg.CommonConfig.CIDRRestrictions.toProtoConfig(),
		ProxyProto:    proto.ProxyProto(cfg.CommonConfig.ProxyProto),
	}
}

func (tcp TCPConfig) tunnelConfig() tunnelConfig {
	return tunnelConfig{
		forwardsTo: tcp.CommonConfig.ForwardsTo,
		proto:      "tcp",
		opts:       tcp.toProtoConfig(),
		extra: proto.BindExtra{
			Metadata: tcp.CommonConfig.Metadata,
		},
	}
}
