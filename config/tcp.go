package config

import (
	"net/http"
	"net/url"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

type TCPEndpointOption interface {
	ApplyTCP(cfg *tcpOptions)
}

type tcpOptionFunc func(cfg *tcpOptions)

func (of tcpOptionFunc) ApplyTCP(cfg *tcpOptions) {
	of(cfg)
}

// TCPEndpoint constructs a new set options for a TCP endpoint.
//
// https://ngrok.com/docs/tcp/
func TCPEndpoint(opts ...TCPEndpointOption) Tunnel {
	cfg := tcpOptions{}
	for _, opt := range opts {
		opt.ApplyTCP(&cfg)
	}
	return &cfg
}

// The options for a TCP edge.
type tcpOptions struct {
	// Common tunnel configuration options.
	commonOpts

	// The TCP address to request for this edge.
	RemoteAddr string
	// An HTTP Server to run traffic on
	// Deprecated: Pass HTTP server refs via session.ListenAndServeHTTP instead.
	httpServer *http.Server
}

// Set the TCP address to request for this edge.
func WithRemoteAddr(addr string) TCPEndpointOption {
	return tcpOptionFunc(func(cfg *tcpOptions) {
		cfg.RemoteAddr = addr
	})
}

func (cfg *tcpOptions) toProtoConfig() *proto.TCPEndpoint {
	return &proto.TCPEndpoint{
		URL:           cfg.URL,
		Addr:          cfg.RemoteAddr,
		IPRestriction: cfg.commonOpts.CIDRRestrictions.toProtoConfig(),
		ProxyProto:    proto.ProxyProto(cfg.commonOpts.ProxyProto),
		TrafficPolicy: cfg.commonOpts.TrafficPolicy,
	}
}

func (cfg tcpOptions) ForwardsTo() string {
	return cfg.commonOpts.getForwardsTo()
}

func (cfg tcpOptions) ForwardsProto() string {
	return "" // Not supported for TCP
}

func (cfg *tcpOptions) WithForwardsTo(url *url.URL) {
	cfg.commonOpts.ForwardsTo = url.Host
}

func (cfg tcpOptions) Extra() proto.BindExtra {
	return proto.BindExtra{
		Name:           cfg.Name,
		Metadata:       cfg.Metadata,
		Description:    cfg.Description,
		Bindings:       cfg.Bindings,
		PoolingEnabled: cfg.PoolingEnabled,
	}
}

func (cfg tcpOptions) Proto() string {
	return "tcp"
}

func (cfg tcpOptions) Opts() any {
	return cfg.toProtoConfig()
}

func (cfg tcpOptions) Labels() map[string]string {
	return nil
}

func (cfg tcpOptions) HTTPServer() *http.Server {
	return cfg.httpServer
}

// compile-time check that we're implementing the proper interfaces.
var _ interface {
	tunnelConfigPrivate
	Tunnel
} = (*tcpOptions)(nil)
