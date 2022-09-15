package libngrok

import (
	"crypto/x509"
	"encoding/pem"

	"github.com/ngrok/libngrok-go/internal/pb_agent"
	"github.com/ngrok/libngrok-go/internal/tunnel/proto"
)

type TLSCommon[T any] struct {
	parent *T

	Domain      string
	MutualTLSCA []*x509.Certificate
}

func (cfg *TLSCommon[T]) WithDomain(name string) *T {
	cfg.Domain = name
	return cfg.parent
}

func (cfg *TLSCommon[T]) WithMutualTLSCA(certs ...*x509.Certificate) *T {
	cfg.MutualTLSCA = append(cfg.MutualTLSCA, certs...)
	return cfg.parent
}

func (cfg *TLSCommon[T]) toProtoConfig() *pb_agent.MiddlewareConfiguration_MutualTLS {
	if cfg == nil || cfg.MutualTLSCA == nil {
		return nil
	}
	opts := &pb_agent.MiddlewareConfiguration_MutualTLS{}
	for _, cert := range cfg.MutualTLSCA {
		opts.MutualTLSCA = append(opts.MutualTLSCA, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})...)
	}
	return opts
}

type TLSConfig struct {
	TLSCommon[TLSConfig]
	CommonConfig[TLSConfig]

	KeyPEM  []byte
	CertPEM []byte
}

func (cfg *TLSConfig) WithEdgeTermination(certPEM, keyPEM []byte) *TLSConfig {
	cfg.CertPEM = certPEM
	cfg.KeyPEM = keyPEM
	return cfg
}

func TLSOptions() *TLSConfig {
	opts := &TLSConfig{}
	opts.TLSCommon = TLSCommon[TLSConfig]{
		parent: opts,
	}
	opts.CommonConfig = CommonConfig[TLSConfig]{
		parent: opts,
	}
	return opts
}

func (cfg *TLSConfig) toProtoConfig() *proto.TLSOptions {
	opts := &proto.TLSOptions{
		Hostname:   cfg.TLSCommon.Domain,
		ProxyProto: proto.ProxyProto(cfg.CommonConfig.ProxyProto),
	}

	opts.IPRestriction = cfg.CIDRRestrictions.toProtoConfig()

	opts.MutualTLSAtEdge = cfg.TLSCommon.toProtoConfig()

	opts.TLSTermination = &pb_agent.MiddlewareConfiguration_TLSTermination{
		Key:  cfg.KeyPEM,
		Cert: cfg.CertPEM,
	}

	return opts
}

func (cfg *TLSConfig) tunnelConfig() tunnelConfig {
	return tunnelConfig{
		forwardsTo: cfg.ForwardsTo,
		proto:      "tls",
		opts:       cfg.toProtoConfig(),
		extra: proto.BindExtra{
			Metadata: cfg.Metadata,
		},
	}
}
