package config

import (
	"golang.ngrok.com/ngrok/internal/mw"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

type OAuthOption func(cfg *oauthOptions)

// oauthOptions configuration
type oauthOptions struct {
	// The OAuth provider to use
	Provider string
	// Email addresses of users to authorize.
	AllowEmails []string
	// Email domains of users to authorize.
	AllowDomains []string
	// OAuth scopes to request from the provider.
	Scopes []string
	// OAuth custom app ID
	ClientID string
	// OAuth custom app secret
	ClientSecret proto.ObfuscatedString
}

// Construct a new OAuth provider with the given name.
func oauthProvider(name string) *oauthOptions {
	return &oauthOptions{
		Provider: name,
	}
}

// WithOAuthClientID provides a client ID for custom OAuth apps.
func WithOAuthClientID(id string) OAuthOption {
	return func(cfg *oauthOptions) {
		cfg.ClientID = id
	}
}

// WithOAuthClientSecret provides a client secret for custom OAuth apps.
func WithOAuthClientSecret(secret string) OAuthOption {
	return func(cfg *oauthOptions) {
		cfg.ClientSecret = proto.ObfuscatedString(secret)
	}
}

// Append email addresses to the list of allowed emails.
func WithAllowOAuthEmail(addr ...string) OAuthOption {
	return func(cfg *oauthOptions) {
		cfg.AllowEmails = append(cfg.AllowEmails, addr...)
	}
}

// Append email domains to the list of allowed domains.
func WithAllowOAuthDomain(domain ...string) OAuthOption {
	return func(cfg *oauthOptions) {
		cfg.AllowDomains = append(cfg.AllowDomains, domain...)
	}
}

// Append scopes to the list of scopes to request.
func WithOAuthScope(scope ...string) OAuthOption {
	return func(cfg *oauthOptions) {
		cfg.Scopes = append(cfg.Scopes, scope...)
	}
}

func (oauth *oauthOptions) toProtoConfig() *mw.MiddlewareConfiguration_OAuth {
	if oauth == nil {
		return nil
	}

	return &mw.MiddlewareConfiguration_OAuth{
		Provider:     string(oauth.Provider),
		ClientId:     oauth.ClientID,
		ClientSecret: oauth.ClientSecret.PlainText(),
		AllowEmails:  oauth.AllowEmails,
		AllowDomains: oauth.AllowDomains,
		Scopes:       oauth.Scopes,
	}
}

// WithOAuth configures this edge with the the given OAuth provider.
// Overwrites any previously-set OAuth configuration.
//
// https://ngrok.com/docs/http/oauth/
func WithOAuth(provider string, opts ...OAuthOption) HTTPEndpointOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		oauth := oauthProvider(provider)
		for _, opt := range opts {
			opt(oauth)
		}
		cfg.OAuth = oauth
	})
}
