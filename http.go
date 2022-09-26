package libngrok

import (
	"crypto/x509"
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
	CommonConfig *CommonConfig
	TLSCommon    *TLSCommon

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
	opts.TLSCommon = &TLSCommon{}
	opts.CommonConfig = &CommonConfig{}
	return opts
}

func (cfg *HTTPConfig) WithScheme(scheme Scheme) *HTTPConfig {
	cfg.Scheme = scheme
	return cfg
}

func (cfg *HTTPConfig) WithWebsocketTCPConversion() *HTTPConfig {
	cfg.WebsocketTCPConversion = true
	return cfg
}

func (cfg *HTTPConfig) WithCompression() *HTTPConfig {
	cfg.Compression = true
	return cfg
}

func (cfg *HTTPConfig) WithCircuitBreaker(ratio float64) *HTTPConfig {
	cfg.CircuitBreaker = ratio
	return cfg
}

func (cfg *HTTPConfig) WithRequestHeaders(headers *Headers) *HTTPConfig {
	cfg.RequestHeaders = headers
	return cfg
}

func (cfg *HTTPConfig) WithResponseHeaders(headers *Headers) *HTTPConfig {
	cfg.ResponseHeaders = headers
	return cfg
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

func (oauth *OAuth) AllowEmail(addr ...string) *OAuth {
	oauth.AllowEmails = append(oauth.AllowEmails, addr...)
	return oauth
}

func (oauth *OAuth) AllowDomain(domain ...string) *OAuth {
	oauth.AllowDomains = append(oauth.AllowDomains, domain...)
	return oauth
}

func (oauth *OAuth) WithScope(scope ...string) *OAuth {
	oauth.Scopes = append(oauth.Scopes, scope...)
	return oauth
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

func (cfg *HTTPConfig) WithOAuth(oauth *OAuth) *HTTPConfig {
	cfg.OAuth = oauth
	return cfg
}

type BasicAuth struct {
	Username, Password string
}

func (cfg *HTTPConfig) WithBasicAuth(username, password string) *HTTPConfig {
	return cfg.WithBasicAuthCreds(BasicAuth{username, password})
}

func (cfg *HTTPConfig) WithBasicAuthCreds(credential ...BasicAuth) *HTTPConfig {
	cfg.BasicAuth = append(cfg.BasicAuth, credential...)
	return cfg
}

type WebhookVerification struct {
	Provider string
	Secret   string
}

func (cfg *HTTPConfig) WithWebhookVerification(provider string, secret string) *HTTPConfig {
	cfg.WebhookVerification = &WebhookVerification{
		Provider: provider,
		Secret:   secret,
	}
	return cfg
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

func (cfg *HTTPConfig) WithDomain(name string) *HTTPConfig {
	cfg.TLSCommon = cfg.TLSCommon.WithDomain(name)
	return cfg
}

func (cfg *HTTPConfig) WithMutualTLSCA(certs ...*x509.Certificate) *HTTPConfig {
	cfg.TLSCommon = cfg.TLSCommon.WithMutualTLSCA(certs...)
	return cfg
}

func (cfg *HTTPConfig) WithProxyProto(version ProxyProtoVersion) *HTTPConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithProxyProto(version)
	return cfg
}

func (cfg *HTTPConfig) WithMetadata(meta string) *HTTPConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithMetadata(meta)
	return cfg
}

func (cfg *HTTPConfig) WithForwardsTo(address string) *HTTPConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithForwardsTo(address)
	return cfg
}

func (cfg *HTTPConfig) WithCIDRRestriction(set ...*CIDRRestriction) *HTTPConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithCIDRRestriction(set...)
	return cfg
}

func (cfg *HTTPConfig) toProtoConfig() *proto.HTTPOptions {
	opts := &proto.HTTPOptions{
		Hostname: cfg.TLSCommon.Domain,
	}

	if cfg.Compression {
		opts.Compression = &pb_agent.MiddlewareConfiguration_Compression{}
	}

	if cfg.WebsocketTCPConversion {
		opts.WebsocketTCPConverter = &pb_agent.MiddlewareConfiguration_WebsocketTCPConverter{}
	}

	if cfg.CircuitBreaker != 0 {
		opts.CircuitBreaker = &pb_agent.MiddlewareConfiguration_CircuitBreaker{
			ErrorThreshold: cfg.CircuitBreaker,
		}
	}

	opts.MutualTLSCA = cfg.TLSCommon.toProtoConfig()

	opts.ProxyProto = proto.ProxyProto(cfg.CommonConfig.ProxyProto)

	opts.RequestHeaders = cfg.RequestHeaders.toProtoConfig()
	opts.ResponseHeaders = cfg.ResponseHeaders.toProtoConfig()
	if len(cfg.BasicAuth) > 0 {
		opts.BasicAuth = &pb_agent.MiddlewareConfiguration_BasicAuth{}
		for _, c := range cfg.BasicAuth {
			opts.BasicAuth.Credentials = append(opts.BasicAuth.Credentials, c.toProtoConfig())
		}
	}
	opts.OAuth = cfg.OAuth.toProtoConfig()
	opts.WebhookVerification = cfg.WebhookVerification.toProtoConfig()
	opts.IPRestriction = cfg.CommonConfig.CIDRRestrictions.toProtoConfig()

	return opts
}

func (cfg *HTTPConfig) applyTunnelConfig(tcfg *tunnelConfig) {
	if cfg.Scheme == "" {
		cfg.Scheme = SchemeHTTPS
	}

	cfg.CommonConfig.applyTunnelConfig(tcfg)

	tcfg.proto = string(cfg.Scheme)
	tcfg.opts = cfg.toProtoConfig()
}
