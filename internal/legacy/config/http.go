package config

import (
	"net/http"
	"net/url"

	"golang.ngrok.com/ngrok/v2/internal/tunnel/proto"
)

type HTTPEndpointOption interface {
	ApplyHTTP(cfg *httpOptions)
}

type httpOptionFunc func(cfg *httpOptions)

func (of httpOptionFunc) ApplyHTTP(cfg *httpOptions) {
	of(cfg)
}

// HTTPEndpoint constructs a new set options for a HTTP endpoint.
//
// https://ngrok.com/docs/http/
func HTTPEndpoint(opts ...HTTPEndpointOption) Tunnel {
	cfg := httpOptions{}
	for _, opt := range opts {
		opt.ApplyHTTP(&cfg)
	}
	return &cfg
}

type httpOptions struct {
	// Common tunnel configuration options.
	commonOpts

	// The scheme that this edge should use.
	// Defaults to [SchemeHTTPS].
	Scheme Scheme

	// If non-nil, start a goroutine which runs this http server
	// accepting connections from the http tunnel
	// Deprecated: Pass HTTP server refs via session.ListenAndServeHTTP instead.
	httpServer *http.Server

	// Auto-rewrite host header on ListenAndForward?
	RewriteHostHeader bool
}

func (cfg *httpOptions) toProtoConfig() *proto.HTTPEndpoint {
	opts := &proto.HTTPEndpoint{
		URL: cfg.URL,
	}

	opts.ProxyProto = proto.ProxyProto(cfg.commonOpts.ProxyProto)

	opts.TrafficPolicy = cfg.TrafficPolicy

	return opts
}

func (cfg httpOptions) ForwardsProto() string {
	return cfg.commonOpts.ForwardsProto
}

func (cfg httpOptions) ForwardsTo() string {
	return cfg.commonOpts.getForwardsTo()
}

func (cfg *httpOptions) WithForwardsTo(url *url.URL) {
	cfg.commonOpts.ForwardsTo = url.Host
}

func (cfg httpOptions) Extra() proto.BindExtra {
	return proto.BindExtra{
		Name:           cfg.Name,
		Metadata:       cfg.Metadata,
		Description:    cfg.Description,
		Bindings:       cfg.Bindings,
		PoolingEnabled: cfg.PoolingEnabled,
	}
}

func (cfg httpOptions) Proto() string {
	if cfg.Scheme == "" {
		return string(SchemeHTTPS)
	}
	return string(cfg.Scheme)
}

func (cfg httpOptions) Opts() any {
	return cfg.toProtoConfig()
}

func (cfg httpOptions) Labels() map[string]string {
	return nil
}

func (cfg httpOptions) HTTPServer() *http.Server {
	return cfg.httpServer
}

// compile-time check that we're implementing the proper interfaces.
var _ interface {
	tunnelConfigPrivate
	Tunnel
} = (*httpOptions)(nil)
