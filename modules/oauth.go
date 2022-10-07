package modules

import "github.com/ngrok/ngrok-go/internal/pb_agent"

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

// WithOAuth configures this edge with the the given OAuth provider.
// Overwrites any previously-set OAuth configuration.
func WithOAuth(oauth *OAuth) HTTPOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.OAuth = oauth
	})
}
