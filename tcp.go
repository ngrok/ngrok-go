package libngrok

import "github.com/ngrok/libngrok-go/internal/tunnel/proto"

type TCPConfig struct {
	CommonConfig[TCPConfig]
	RemoteAddr string
}

func TCPOptions() *TCPConfig {
	opts := &TCPConfig{}
	opts.CommonConfig = CommonConfig[TCPConfig]{
		parent: opts,
	}
	return opts
}

func (tcp *TCPConfig) WithRemoteAddr(addr string) *TCPConfig {
	tcp.RemoteAddr = addr
	return tcp
}

func (tcp *TCPConfig) toProtoConfig() *proto.TCPOptions {
	return &proto.TCPOptions{
		Addr:          tcp.RemoteAddr,
		IPRestriction: tcp.parent.CIDRRestrictions.toProtoConfig(),
		ProxyProto:    proto.ProxyProto(tcp.parent.ProxyProto),
	}
}

func (tcp *TCPConfig) tunnelConfig() tunnelConfig {
	return tunnelConfig{
		forwardsTo: tcp.ForwardsTo,
		proto:      "tcp",
		opts:       tcp.toProtoConfig(),
		extra: proto.BindExtra{
			Metadata: tcp.Metadata,
		},
	}
}
