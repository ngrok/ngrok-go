package config

import (
	"crypto/x509"
	"net/http"
	"net/url"

	"golang.ngrok.com/ngrok/internal/mw"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
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

	// The fqdn to request for this edge
	Domain string

	// Note: these are "the old way", and shouldn't actually be used. Their
	// setters are both deprecated.
	Hostname  string
	Subdomain string

	// If non-nil, start a goroutine which runs this http server
	// accepting connections from the http tunnel
	// Deprecated: Pass HTTP server refs via session.ListenAndServeHTTP instead.
	httpServer *http.Server

	// Certificates to use for client authentication at the ngrok edge.
	MutualTLSCA []*x509.Certificate
	// Enable gzip compression for HTTP responses.
	Compression bool
	// Convert incoming websocket connections to TCP-like streams.
	WebsocketTCPConversion bool
	// Reject requests when 5XX responses exceed this ratio.
	// Disabled when 0.
	CircuitBreaker float64

	// Headers to be added to or removed from all requests at the ngrok edge.
	RequestHeaders *headers
	// Headers to be added to or removed from all responses at the ngrok edge.
	ResponseHeaders *headers

	// Auto-rewrite host header on ListenAndForward?
	RewriteHostHeader bool

	// Credentials for basic authentication.
	// If empty, basic authentication is disabled.
	BasicAuth []basicAuth
	// OAuth configuration.
	// If nil, OAuth is disabled.
	OAuth *oauthOptions
	// OIDC configuration.
	// If nil, OIDC is disabled.
	OIDC *oidcOptions
	// WebhookVerification configuration.
	// If nil, WebhookVerification is disabled.
	WebhookVerification *webhookVerification
	// UserAgentFilter configuration
	// If nil, UserAgentFilter is disabled
	UserAgentFilter *userAgentFilter
}

func (cfg *httpOptions) toProtoConfig() *proto.HTTPEndpoint {
	opts := &proto.HTTPEndpoint{
		URL:       cfg.URL,
		Domain:    cfg.Domain,
		Hostname:  cfg.Hostname,
		Subdomain: cfg.Subdomain,
	}

	if cfg.Compression {
		opts.Compression = &mw.MiddlewareConfiguration_Compression{}
	}

	if cfg.WebsocketTCPConversion {
		opts.WebsocketTCPConverter = &mw.MiddlewareConfiguration_WebsocketTCPConverter{}
	}

	if cfg.CircuitBreaker != 0 {
		opts.CircuitBreaker = &mw.MiddlewareConfiguration_CircuitBreaker{
			ErrorThreshold: cfg.CircuitBreaker,
		}
	}

	opts.MutualTLSCA = mutualTLSEndpointOption(cfg.MutualTLSCA).toProtoConfig()

	opts.ProxyProto = proto.ProxyProto(cfg.commonOpts.ProxyProto)

	opts.RequestHeaders = cfg.RequestHeaders.toProtoConfig()
	opts.ResponseHeaders = cfg.ResponseHeaders.toProtoConfig()
	if len(cfg.BasicAuth) > 0 {
		opts.BasicAuth = &mw.MiddlewareConfiguration_BasicAuth{}
		for _, c := range cfg.BasicAuth {
			opts.BasicAuth.Credentials = append(opts.BasicAuth.Credentials, c.toProtoConfig())
		}
	}
	opts.OAuth = cfg.OAuth.toProtoConfig()
	opts.OIDC = cfg.OIDC.toProtoConfig()
	opts.WebhookVerification = cfg.WebhookVerification.toProtoConfig()
	opts.IPRestriction = cfg.commonOpts.CIDRRestrictions.toProtoConfig()
	opts.UserAgentFilter = cfg.UserAgentFilter.toProtoConfig()
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
	if cfg.RewriteHostHeader {
		WithRequestHeader("host", url.Host).ApplyHTTP(cfg)
	}
}

func (cfg httpOptions) Extra() proto.BindExtra {
	return proto.BindExtra{
		Name:          cfg.Name,
		Metadata:      cfg.Metadata,
		Description:   cfg.Description,
		Bindings:      cfg.Bindings,
		AllowsPooling: cfg.AllowsPooling,
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
