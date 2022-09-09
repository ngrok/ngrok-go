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

type TLSKeypair struct {
	KeyPEM  []byte
	CertPEM []byte
}

func (kp *TLSKeypair) toProtoConfig() *pb_agent.MiddlewareConfiguration_TLSTermination {
	if kp == nil {
		return nil
	}

	return &pb_agent.MiddlewareConfiguration_TLSTermination{
		Key:  kp.KeyPEM,
		Cert: kp.CertPEM,
	}
}

type TLSConfig struct {
	TLSCommon[TLSConfig]
	CommonConfig[TLSConfig]

	TerminateKeypair *TLSKeypair
}

func (cfg *TLSConfig) WithEdgeTermination(certPEM, keyPEM []byte) *TLSConfig {
	cfg.TerminateKeypair = &TLSKeypair{
		CertPEM: certPEM,
		KeyPEM:  keyPEM,
	}
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

func (tls *TLSConfig) toProtoConfig() *proto.TLSOptions {
	opts := &proto.TLSOptions{
		Hostname:   tls.TLSCommon.Domain,
		ProxyProto: proto.ProxyProto(tls.CommonConfig.ProxyProto),
	}

	opts.IPRestriction = tls.CIDRRestrictions.toProtoConfig()

	opts.MutualTLSAtEdge = tls.TLSCommon.toProtoConfig()

	opts.TLSTermination = tls.TerminateKeypair.toProtoConfig()

	return opts
}

func (tls *TLSConfig) tunnelConfig() tunnelConfig {
	return tunnelConfig{
		forwardsTo: tls.ForwardsTo,
		proto:      "tls",
		opts:       tls.toProtoConfig(),
		extra: proto.BindExtra{
			Metadata: tls.Metadata,
		},
	}
}
