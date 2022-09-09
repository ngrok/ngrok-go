package libngrok

import (
	"fmt"

	"github.com/ngrok/libngrok-go/internal/pb_agent"
	"github.com/ngrok/libngrok-go/internal/tunnel/proto"
)

type Scheme string

const SchemeHTTP = Scheme("http")
const SchemeHTTPS = Scheme("https")

type Headers struct {
	Added   map[string]string
	Removed []string
}

func (h *Headers) Add(name, value string) *Headers {
	h.Added[name] = value
	return h
}

func (h *Headers) Remove(name string) *Headers {
	h.Removed = append(h.Removed, name)
	return h
}

func (h *Headers) toProtoConfig() *pb_agent.MiddlewareConfiguration_Headers {
	if h == nil {
		return nil
	}

	headers := &pb_agent.MiddlewareConfiguration_Headers{
		Remove: h.Removed,
	}

	for k, v := range h.Added {
		headers.Add = append(headers.Add, fmt.Sprintf("%s:%s", k, v))
	}

	return headers
}

func HTTPHeaders() *Headers {
	return &Headers{
		Added:   map[string]string{},
		Removed: []string{},
	}
}

type HTTPConfig struct {
	CommonConfig[HTTPConfig]
	TLSCommon[HTTPConfig]

	Scheme                 Scheme
	Compression            bool
	WebsocketTCPConversion bool
	CircuitBreaker         float64

	RequestHeaders  *Headers
	ResponseHeaders *Headers

	BasicAuth           []BasicAuth
	OAuth               *OAuth
	WebhookVerification *WebhookVerification
}

func HTTPOptions() *HTTPConfig {
	opts := &HTTPConfig{}
	opts.TLSCommon = TLSCommon[HTTPConfig]{
		parent: opts,
	}
	opts.CommonConfig = CommonConfig[HTTPConfig]{
		parent: opts,
	}
	return opts
}

func (http *HTTPConfig) WithScheme(scheme Scheme) *HTTPConfig {
	http.Scheme = scheme
	return http
}

func (http *HTTPConfig) WithWebsocketTCPConversion() *HTTPConfig {
	http.WebsocketTCPConversion = true
	return http
}

func (http *HTTPConfig) WithCompression() *HTTPConfig {
	http.Compression = true
	return http
}

func (http *HTTPConfig) WithCircuitBreaker(ratio float64) *HTTPConfig {
	http.CircuitBreaker = ratio
	return http
}

func (http *HTTPConfig) WithRequestHeaders(headers *Headers) *HTTPConfig {
	http.RequestHeaders = headers
	return http
}

func (http *HTTPConfig) WithResponseHeaders(headers *Headers) *HTTPConfig {
	http.ResponseHeaders = headers
	return http
}

func (ba BasicAuth) toProtoConfig() *pb_agent.MiddlewareConfiguration_BasicAuthCredential {
	return &pb_agent.MiddlewareConfiguration_BasicAuthCredential{
		CleartextPassword: ba.Password,
		Username:          ba.Username,
	}
}

type OAuth struct {
	Provider     string
	AllowEmails  []string
	AllowDomains []string
	Scopes       []string
}

func OAuthProvider(name string) *OAuth {
	return &OAuth{
		Provider: name,
	}
}

func (p *OAuth) AllowEmail(addr ...string) *OAuth {
	p.AllowEmails = append(p.AllowEmails, addr...)
	return p
}

func (p *OAuth) AllowDomain(domain ...string) *OAuth {
	p.AllowDomains = append(p.AllowDomains, domain...)
	return p
}

func (p *OAuth) WithScope(scope ...string) *OAuth {
	p.Scopes = append(p.Scopes, scope...)
	return p
}

func (http *HTTPConfig) WithOAuth(cfg *OAuth) *HTTPConfig {
	http.OAuth = cfg
	return http
}

func (oauth *OAuth) toProtoConfig() *pb_agent.MiddlewareConfiguration_OAuth {
	if oauth == nil {
		return nil
	}

	return &pb_agent.MiddlewareConfiguration_OAuth{
		Provider:     string(oauth.Provider),
		AllowEmails:  oauth.AllowEmails,
		AllowDomains: oauth.AllowDomains,
		Scopes:       oauth.Scopes,
	}
}

type BasicAuth struct {
	Username, Password string
}

func (http *HTTPConfig) WithBasicAuth(username, password string) *HTTPConfig {
	return http.WithBasicAuthCreds(BasicAuth{username, password})
}

func (http *HTTPConfig) WithBasicAuthCreds(credential ...BasicAuth) *HTTPConfig {
	http.BasicAuth = append(http.BasicAuth, credential...)
	return http
}

type WebhookVerification struct {
	Provider string
	Secret   string
}

func (http *HTTPConfig) WithWebhookVerification(provider string, secret string) *HTTPConfig {
	http.WebhookVerification = &WebhookVerification{
		Provider: provider,
		Secret:   secret,
	}
	return http
}

func (wv *WebhookVerification) toProtoConfig() *pb_agent.MiddlewareConfiguration_WebhookVerification {
	if wv == nil {
		return nil
	}
	return &pb_agent.MiddlewareConfiguration_WebhookVerification{
		Provider: wv.Provider,
		Secret:   wv.Secret,
	}
}

func (http *HTTPConfig) toProtoConfig() *proto.HTTPOptions {
	opts := &proto.HTTPOptions{
		Hostname: http.Domain,
	}

	if http.Compression {
		opts.Compression = &pb_agent.MiddlewareConfiguration_Compression{}
	}

	if http.WebsocketTCPConversion {
		opts.WebsocketTCPConverter = &pb_agent.MiddlewareConfiguration_WebsocketTCPConverter{}
	}

	if http.CircuitBreaker != 0 {
		opts.CircuitBreaker = &pb_agent.MiddlewareConfiguration_CircuitBreaker{
			ErrorThreshold: http.CircuitBreaker,
		}
	}

	opts.MutualTLSCA = http.TLSCommon.toProtoConfig()

	opts.ProxyProto = proto.ProxyProto(http.ProxyProto)

	opts.RequestHeaders = http.RequestHeaders.toProtoConfig()
	opts.ResponseHeaders = http.ResponseHeaders.toProtoConfig()
	if len(http.BasicAuth) > 0 {
		opts.BasicAuth = &pb_agent.MiddlewareConfiguration_BasicAuth{}
		for _, c := range http.BasicAuth {
			opts.BasicAuth.Credentials = append(opts.BasicAuth.Credentials, c.toProtoConfig())
		}
	}
	opts.OAuth = http.OAuth.toProtoConfig()
	opts.WebhookVerification = http.WebhookVerification.toProtoConfig()
	opts.IPRestriction = http.CIDRRestrictions.toProtoConfig()

	return opts
}

func (cfg *HTTPConfig) tunnelConfig() tunnelConfig {
	if cfg.Scheme == "" {
		cfg.Scheme = SchemeHTTPS
	}
	return tunnelConfig{
		forwardsTo: cfg.ForwardsTo,
		proto:      string(cfg.Scheme),
		opts:       cfg.toProtoConfig(),
		extra: proto.BindExtra{
			Metadata: cfg.Metadata,
		},
	}
}
