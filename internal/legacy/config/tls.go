package config

import (
	"net/http"
	"net/url"

	"golang.ngrok.com/ngrok/v2/internal/pb"
	"golang.ngrok.com/ngrok/v2/internal/tunnel/proto"
)

type TLSEndpointOption interface {
	ApplyTLS(cfg *tlsOptions)
}

type tlsOptionFunc func(cfg *tlsOptions)

func (of tlsOptionFunc) ApplyTLS(cfg *tlsOptions) {
	of(cfg)
}

// TLSEndpoint constructs a new set options for a TLS endpoint.
//
// https://ngrok.com/docs/tls/
func TLSEndpoint(opts ...TLSEndpointOption) Tunnel {
	cfg := tlsOptions{}
	for _, opt := range opts {
		opt.ApplyTLS(&cfg)
	}
	return &cfg
}

// The options for TLS edges.
type tlsOptions struct {
	// Common tunnel options
	commonOpts

	// True if the TLS connection should be terminated at the ngrok edge.
	terminateAtEdge bool
	// The key to use for TLS termination at the ngrok edge in PEM format.
	KeyPEM []byte
	// The certificate to use for TLS termination at the ngrok edge in PEM
	// format.
	CertPEM []byte

	// An HTTP Server to run traffic on
	// Deprecated: Pass HTTP server refs via session.ListenAndServeHTTP instead.
	httpServer *http.Server
}

func (cfg *tlsOptions) toProtoConfig() *proto.TLSEndpoint {
	opts := &proto.TLSEndpoint{
		URL:        cfg.URL,
		ProxyProto: proto.ProxyProto(cfg.ProxyProto),
	}

	opts.TrafficPolicy = cfg.commonOpts.TrafficPolicy

	// When terminate-at-edge is set the TLSTermination must be sent even if the key and cert are nil,
	// this will default to the ngrok edge's automatically provisioned keypair.
	if cfg.terminateAtEdge {
		opts.TLSTermination = &pb.MiddlewareConfiguration_TLSTermination{
			Key:  cfg.KeyPEM,
			Cert: cfg.CertPEM,
		}
	}

	return opts
}

func (cfg tlsOptions) ForwardsProto() string {
	return "" // Not supported for TLS
}

func (cfg tlsOptions) ForwardsTo() string {
	return cfg.commonOpts.getForwardsTo()
}

func (cfg *tlsOptions) WithForwardsTo(url *url.URL) {
	cfg.commonOpts.ForwardsTo = url.Host
}

func (cfg tlsOptions) Extra() proto.BindExtra {
	return proto.BindExtra{
		Name:           cfg.Name,
		Metadata:       cfg.Metadata,
		Description:    cfg.Description,
		Bindings:       cfg.Bindings,
		PoolingEnabled: cfg.PoolingEnabled,
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

func (cfg tlsOptions) HTTPServer() *http.Server {
	return cfg.httpServer
}

// compile-time check that we're implementing the proper interfaces.
var _ interface {
	tunnelConfigPrivate
	Tunnel
} = (*tlsOptions)(nil)
