package modules

import (
	"crypto/x509"

	"github.com/ngrok/ngrok-go/internal/pb_agent"
	"github.com/ngrok/ngrok-go/internal/tunnel/proto"
)

type TLSOption interface {
	ApplyTLS(cfg *tlsOptions)
}

type tlsOptionFunc func(cfg *tlsOptions)

func (of tlsOptionFunc) ApplyTLS(cfg *tlsOptions) {
	of(cfg)
}

// Construct a new set of HTTP tunnel options.
func TLSOptions(opts ...TLSOption) TunnelOptions {
	cfg := tlsOptions{}
	for _, opt := range opts {
		opt.ApplyTLS(&cfg)
	}
	return cfg
}

// The options for TLS edges.
type tlsOptions struct {
	// Common tunnel options
	commonOpts

	// The domain to request for this edge.
	Domain string

	// Certificates to use for client authentication at the ngrok edge.
	MutualTLSCA []*x509.Certificate

	// The key to use for TLS termination at the ngrok edge in PEM format.
	KeyPEM []byte
	// The certificate to use for TLS termination at the ngrok edge in PEM
	// format.
	CertPEM []byte
}

func (cfg *tlsOptions) toProtoConfig() *proto.TLSOptions {
	opts := &proto.TLSOptions{
		Hostname:   cfg.Domain,
		ProxyProto: proto.ProxyProto(cfg.ProxyProto),
	}

	opts.IPRestriction = cfg.commonOpts.CIDRRestrictions.toProtoConfig()

	opts.MutualTLSAtEdge = mutualTLSOption(cfg.MutualTLSCA).toProtoConfig()

	opts.TLSTermination = &pb_agent.MiddlewareConfiguration_TLSTermination{
		Key:  cfg.KeyPEM,
		Cert: cfg.CertPEM,
	}

	return opts
}

func (cfg tlsOptions) tunnelOptions() {}

func (cfg tlsOptions) ForwardsTo() string {
	return cfg.commonOpts.getForwardsTo()
}
func (cfg tlsOptions) Extra() proto.BindExtra {
	return proto.BindExtra{
		Metadata: cfg.Metadata,
	}
}
func (cfg tlsOptions) Proto() string {
	return "tls"
}
func (cfg tlsOptions) Opts() any {
	return cfg.toProtoConfig()
}
func (cfg tlsOptions) Labels() map[string]string {
	return nil
}
