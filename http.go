package ngrok

import (
	"crypto/x509"
	"fmt"

	"github.com/ngrok/ngrok-go/internal/pb_agent"
	"github.com/ngrok/ngrok-go/internal/tunnel/proto"
)

// A URL scheme.
type Scheme string

// The 'http' URL scheme.
const SchemeHTTP = Scheme("http")

// The 'https' URL scheme.
const SchemeHTTPS = Scheme("https")

// HTTP headers to modify at the ngrok edge.
type Headers struct {
	// Headers to add to requests or responses at the ngrok edge.
	Added map[string]string
	// Header names to remove from requests or responses at the ngrok edge.
	Removed []string
}

// Add a header to all requests or responses at the ngrok edge.
// Inserts an entry into the [Headers].Added map.
func (h *Headers) Add(name, value string) *Headers {
	if h.Added == nil {
		h.Added = map[string]string{}
	}

	h.Added[name] = value
	return h
}

// Add a header to be removed from all requests or responses at the ngrok edge.
// Appends to the [Headers].Removed slice.
func (h *Headers) Remove(name ...string) *Headers {
	h.Removed = append(h.Removed, name...)
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

// Construct a new set of [Headers] for modification at the ngrok edge.
func HTTPHeaders() *Headers {
	return &Headers{
		Added:   map[string]string{},
		Removed: []string{},
	}
}

func (h *Headers) merge(other *Headers) *Headers {
	if h == nil {
		h = HTTPHeaders()
	}

	if other == nil {
		return h
	}

	for k, v := range other.Added {
		if existing, ok := h.Added[k]; ok {
			v = fmt.Sprintf("%s;%s", existing, v)
		}
		h.Added[k] = v
	}

	h.Removed = append(h.Removed, other.Removed...)

	return h
}

// The options for an HTTP or HTTPS edge.
type HTTPConfig struct {
	// Common tunnel configuration options.
	CommonConfig *CommonConfig
	// Common TLS configuration options.
	TLSCommon *TLSCommon

	// The scheme that this edge should use.
	// Defaults to [SchemeHTTPS].
	Scheme Scheme
	// Enable gzip compression for HTTP responses.
	Compression bool
	// Convert incoming websocket connections to TCP-like streams.
	WebsocketTCPConversion bool
	// Reject requests when 5XX responses exceed this ratio.
	// Disabled when 0.
	CircuitBreaker float64

	// Headers to be added to or removed from all requests at the ngrok edge.
	RequestHeaders *Headers
	// Headers to be added to or removed from all responses at the ngrok edge.
	ResponseHeaders *Headers

	// Credentials for basic authentication.
	// If empty, basic authentication is disabled.
	BasicAuth []BasicAuth
	// OAuth configuration.
	// If nil, OAuth is disabled.
	OAuth *OAuth
	// WebhookVerification configuration.
	// If nil, WebhookVerification is disabled.
	WebhookVerification *WebhookVerification
}

// Construct a new set of HTTP tunnel options.
func HTTPOptions() *HTTPConfig {
	opts := &HTTPConfig{}
	opts.TLSCommon = &TLSCommon{}
	opts.CommonConfig = &CommonConfig{}
	return opts
}

// Use the provided scheme for this edge.
// Sets the [HTTPConfig].Scheme field.
func (cfg *HTTPConfig) WithScheme(scheme Scheme) *HTTPConfig {
	cfg.Scheme = scheme
	return cfg
}

// Enable the websocket-to-tcp converter.
// Sets the [HTTPConfig].WebsocketTCPConversion field to true.
func (cfg *HTTPConfig) WithWebsocketTCPConversion() *HTTPConfig {
	cfg.WebsocketTCPConversion = true
	return cfg
}

// Enable gzip compression.
// Sets the [HTTPConfig].Compression field to true.
func (cfg *HTTPConfig) WithCompression() *HTTPConfig {
	cfg.Compression = true
	return cfg
}

// Set the 5XX response ratio at which the ngrok edge will stop sending requests
// to this tunnel.
// Sets the [HTTPConfig].CircuitBreaker ratio.
func (cfg *HTTPConfig) WithCircuitBreaker(ratio float64) *HTTPConfig {
	cfg.CircuitBreaker = ratio
	return cfg
}

// Configure request headers for addition or removal at the ngrok edge.
// Merges with any existing [HTTPConfig].RequestHeaders.
func (cfg *HTTPConfig) WithRequestHeaders(headers *Headers) *HTTPConfig {
	cfg.RequestHeaders = cfg.RequestHeaders.merge(headers)
	return cfg
}

// Configure response headers for addition or removal at the ngrok edge.
// Merges with any existing [HTTPConfig].ResponseHeaders.
func (cfg *HTTPConfig) WithResponseHeaders(headers *Headers) *HTTPConfig {
	cfg.ResponseHeaders = cfg.ResponseHeaders.merge(headers)
	return cfg
}

func (ba BasicAuth) toProtoConfig() *pb_agent.MiddlewareConfiguration_BasicAuthCredential {
	return &pb_agent.MiddlewareConfiguration_BasicAuthCredential{
		CleartextPassword: ba.Password,
		Username:          ba.Username,
	}
}

// OAuth configuration
type OAuth struct {
	// The OAuth provider to use
	Provider string
	// Email addresses of users to authorize.
	AllowEmails []string
	// Email domains of users to authorize.
	AllowDomains []string
	// OAuth scopes to request from the provider.
	Scopes []string
}

// Construct a new OAuth provider with the given name.
func OAuthProvider(name string) *OAuth {
	return &OAuth{
		Provider: name,
	}
}

// Append email addresses to the list of allowed emails.
func (oauth *OAuth) AllowEmail(addr ...string) *OAuth {
	oauth.AllowEmails = append(oauth.AllowEmails, addr...)
	return oauth
}

// Append email domains to the list of allowed domains.
func (oauth *OAuth) AllowDomain(domain ...string) *OAuth {
	oauth.AllowDomains = append(oauth.AllowDomains, domain...)
	return oauth
}

// Append scopes to the list of scopes to request.
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

// Configure this edge with the the given OAuth provider.
// Overwrites any previously-set OAuth configuration.
func (cfg *HTTPConfig) WithOAuth(oauth *OAuth) *HTTPConfig {
	cfg.OAuth = oauth
	return cfg
}

// A set of credentials for basic authentication.
type BasicAuth struct {
	// The username for basic authentication.
	Username string
	// The password for basic authentication.
	// Must be at least eight characters.
	Password string
}

// Add the provided credentials to the list of basic authentication credentials.
func (cfg *HTTPConfig) WithBasicAuth(username, password string) *HTTPConfig {
	return cfg.WithBasicAuthCreds(BasicAuth{username, password})
}

// Add a list of username/password pairs to the list of basic authentication
// credentials.
func (cfg *HTTPConfig) WithBasicAuthCreds(credential ...BasicAuth) *HTTPConfig {
	cfg.BasicAuth = append(cfg.BasicAuth, credential...)
	return cfg
}

// Configuration for webhook verification.
type WebhookVerification struct {
	// The webhook provider
	Provider string
	// The secret for verifying webhooks from this provider.
	Secret string
}

// Configure webhook vericiation for this edge.
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

// Set the domain to request for this edge.
func (cfg *HTTPConfig) WithDomain(name string) *HTTPConfig {
	cfg.TLSCommon = cfg.TLSCommon.WithDomain(name)
	return cfg
}

// Add a list of [x509.Certificate]'s to use for mutual TLS authentication.
// These will be used to authenticate client certificates for requests at the
// ngrok edge.
func (cfg *HTTPConfig) WithMutualTLSCA(certs ...*x509.Certificate) *HTTPConfig {
	cfg.TLSCommon = cfg.TLSCommon.WithMutualTLSCA(certs...)
	return cfg
}

// Use the provided PROXY protocol version for connections over this tunnel.
// Sets the [CommonConfig].ProxyProto field.
func (cfg *HTTPConfig) WithProxyProto(version ProxyProtoVersion) *HTTPConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithProxyProto(version)
	return cfg
}

// Use the provided opaque metadata string for this tunnel.
// Sets the [CommonConfig].Metadata field.
func (cfg *HTTPConfig) WithMetadata(meta string) *HTTPConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithMetadata(meta)
	return cfg
}

// Use the provided backend as the tunnel's ForwardsTo string.
// Sets the [CommonConfig].ForwardsTo field.
func (cfg *HTTPConfig) WithForwardsTo(backend string) *HTTPConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithForwardsTo(backend)
	return cfg
}

// Add the provided [CIDRRestriction] to the tunnel.
// Concatenates all provided Allowed and Denied lists with the existing ones.
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
