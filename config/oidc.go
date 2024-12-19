package config

import (
	"golang.ngrok.com/ngrok/internal/mw"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

type OIDCOption func(cfg *oidcOptions)

type oidcOptions struct {
	IssuerURL    string
	ClientID     string
	ClientSecret proto.ObfuscatedString
	AllowEmails  []string
	AllowDomains []string
	Scopes       []string
}

func (oidc *oidcOptions) toProtoConfig() *mw.MiddlewareConfiguration_OIDC {
	if oidc == nil {
		return nil
	}

	return &mw.MiddlewareConfiguration_OIDC{
		IssuerUrl:    oidc.IssuerURL,
		ClientId:     oidc.ClientID,
		ClientSecret: oidc.ClientSecret.PlainText(),
		AllowEmails:  oidc.AllowEmails,
		AllowDomains: oidc.AllowDomains,
		Scopes:       oidc.Scopes,
	}
}

// WithOIDC configures this edge with the the given OIDC provider.
// Overwrites any previously-set OIDC configuration.
//
// https://ngrok.com/docs/http/openid-connect/
func WithOIDC(issuerURL string, clientID string, clientSecret string, opts ...OIDCOption) HTTPEndpointOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		oidc := &oidcOptions{
			IssuerURL:    issuerURL,
			ClientID:     clientID,
			ClientSecret: proto.ObfuscatedString(clientSecret),
		}

		for _, opt := range opts {
			opt(oidc)
		}

		cfg.OIDC = oidc
	})
}

// Append email addresses to the list of allowed emails.
func WithAllowOIDCEmail(addr ...string) OIDCOption {
	return func(cfg *oidcOptions) {
		cfg.AllowEmails = append(cfg.AllowEmails, addr...)
	}
}

// Append email domains to the list of allowed domains.
func WithAllowOIDCDomain(domain ...string) OIDCOption {
	return func(cfg *oidcOptions) {
		cfg.AllowDomains = append(cfg.AllowDomains, domain...)
	}
}

// Append scopes to the list of scopes to request.
func WithOIDCScope(scope ...string) OIDCOption {
	return func(cfg *oidcOptions) {
		cfg.Scopes = append(cfg.Scopes, scope...)
	}
}
