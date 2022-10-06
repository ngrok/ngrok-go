package ngrok

import (
	"crypto/x509"
	"encoding/pem"

	"github.com/ngrok/ngrok-go/internal/pb_agent"
	"github.com/ngrok/ngrok-go/internal/tunnel/proto"
)

// Common options for edges that support TLS (HTTPS and TLS).
type TLSCommon struct {
	// The domain to request for this edge.
	Domain string
	// Certificates to use for client authentication at the ngrok edge.
	MutualTLSCA []*x509.Certificate
}

// Set the domain to request for this edge.
func (cfg *TLSCommon) WithDomain(name string) *TLSCommon {
	cfg.Domain = name
	return cfg
}

// Add a list of [x509.Certificate]'s to use for mutual TLS authentication.
// These will be used to authenticate client certificates for requests at the
// ngrok edge.
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

// The options for TLS edges.
type TLSConfig struct {
	// Common tunnel options
	CommonConfig *CommonConfig
	// Common TLS options
	TLSCommon *TLSCommon

	// The key to use for TLS termination at the ngrok edge in PEM format.
	KeyPEM []byte
	// The certificate to use for TLS termination at the ngrok edge in PEM
	// format.
	CertPEM []byte
}

// Set the key and certificate in PEM format for TLS termination at the ngrok
// edge.
func (cfg *TLSConfig) WithTermination(certPEM, keyPEM []byte) *TLSConfig {
	cfg.CertPEM = certPEM
	cfg.KeyPEM = keyPEM
	return cfg
}

// Construct a new set of TLS options.
func TLSOptions() *TLSConfig {
	opts := &TLSConfig{}
	opts.TLSCommon = &TLSCommon{}
	opts.CommonConfig = &CommonConfig{}
	return opts
}

// Set the domain to request for this edge.
func (cfg *TLSConfig) WithDomain(name string) *TLSConfig {
	cfg.TLSCommon = cfg.TLSCommon.WithDomain(name)
	return cfg
}

// Add a list of [x509.Certificate]'s to use for mutual TLS authentication.
// These will be used to authenticate client certificates for requests at the
// ngrok edge.
func (cfg *TLSConfig) WithMutualTLSCA(certs ...*x509.Certificate) *TLSConfig {
	cfg.TLSCommon = cfg.TLSCommon.WithMutualTLSCA(certs...)
	return cfg
}

// Use the provided PROXY protocol version for connections over this tunnel.
// Sets the [CommonConfig].ProxyProto field.
func (cfg *TLSConfig) WithProxyProto(version ProxyProtoVersion) *TLSConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithProxyProto(version)
	return cfg
}

// Use the provided opaque metadata string for this tunnel.
// Sets the [CommonConfig].Metadata field.
func (cfg *TLSConfig) WithMetadata(meta string) *TLSConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithMetadata(meta)
	return cfg
}

// Use the provided backend as the tunnel's ForwardsTo string.
// Sets the [CommonConfig].ForwardsTo field.
func (cfg *TLSConfig) WithForwardsTo(address string) *TLSConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithForwardsTo(address)
	return cfg
}

// Add the provided [CIDRRestriction] to the tunnel.
// Concatenates all provided Allowed and Denied lists with the existing ones.
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
