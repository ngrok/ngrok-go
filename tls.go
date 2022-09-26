package libngrok

import (
	"crypto/x509"
	"encoding/pem"

	"github.com/ngrok/libngrok-go/internal/pb_agent"
	"github.com/ngrok/libngrok-go/internal/tunnel/proto"
)

type TLSCommon struct {
	Domain      string
	MutualTLSCA []*x509.Certificate
}

func (cfg *TLSCommon) WithDomain(name string) *TLSCommon {
	cfg.Domain = name
	return cfg
}

func (cfg *TLSCommon) WithMutualTLSCA(certs ...*x509.Certificate) *TLSCommon {
	cfg.MutualTLSCA = append(cfg.MutualTLSCA, certs...)
	return cfg
}

func (cfg *TLSCommon) toProtoConfig() *pb_agent.MiddlewareConfiguration_MutualTLS {
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
	TLSCommon    *TLSCommon
	CommonConfig *CommonConfig

	KeyPEM  []byte
	CertPEM []byte
}

func (cfg *TLSConfig) WithTermination(certPEM, keyPEM []byte) *TLSConfig {
	cfg.CertPEM = certPEM
	cfg.KeyPEM = keyPEM
	return cfg
}

func TLSOptions() *TLSConfig {
	opts := &TLSConfig{}
	opts.TLSCommon = &TLSCommon{}
	opts.CommonConfig = &CommonConfig{}
	return opts
}

func (cfg *TLSConfig) WithDomain(name string) *TLSConfig {
	cfg.TLSCommon = cfg.TLSCommon.WithDomain(name)
	return cfg
}

func (cfg *TLSConfig) WithMutualTLSCA(certs ...*x509.Certificate) *TLSConfig {
	cfg.TLSCommon = cfg.TLSCommon.WithMutualTLSCA(certs...)
	return cfg
}

func (cfg *TLSConfig) WithProxyProto(version ProxyProtoVersion) *TLSConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithProxyProto(version)
	return cfg
}

func (cfg *TLSConfig) WithMetadata(meta string) *TLSConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithMetadata(meta)
	return cfg
}

func (cfg *TLSConfig) WithForwardsTo(address string) *TLSConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithForwardsTo(address)
	return cfg
}

func (cfg *TLSConfig) WithCIDRRestriction(set ...*CIDRRestriction) *TLSConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithCIDRRestriction(set...)
	return cfg
}

func (cfg *TLSConfig) toProtoConfig() *proto.TLSOptions {
	opts := &proto.TLSOptions{
		Hostname:   cfg.TLSCommon.Domain,
		ProxyProto: proto.ProxyProto(cfg.CommonConfig.ProxyProto),
	}

	opts.IPRestriction = cfg.CommonConfig.CIDRRestrictions.toProtoConfig()

	opts.MutualTLSAtEdge = cfg.TLSCommon.toProtoConfig()

	opts.TLSTermination = &pb_agent.MiddlewareConfiguration_TLSTermination{
		Key:  cfg.KeyPEM,
		Cert: cfg.CertPEM,
	}

	return opts
}

func (cfg *TLSConfig) applyTunnelConfig(tcfg *tunnelConfig) {
	cfg.CommonConfig.applyTunnelConfig(tcfg)

	tcfg.proto = "tls"
	tcfg.opts = cfg.toProtoConfig()
}
