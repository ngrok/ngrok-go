package config

import (
	"golang.ngrok.com/ngrok/internal/mw"
)

// UserAgentFilter is a pair of strings slices that allow/deny traffic to an endpoint
type userAgentFilter struct {
	// slice of regex strings for allowed user agents
	Allow []string
	// slice of regex strings for denied user agents
	Deny []string
}

// WithAllowUserAgentFilter is a deprecated alias for [WithAllowUserAgent].
//
// Deprecated: use [WithAllowUserAgent] instead.
func WithAllowUserAgentFilter(allow ...string) HTTPEndpointOption {
	return WithAllowUserAgent(allow...)
}

// WithDenyUserAgentFilter is a deprecated alias for [WithDenyUserAgent].
//
// Deprecated: use [WithDenyUserAgent] instead.
func WithDenyUserAgentFilter(allow ...string) HTTPEndpointOption {
	return WithDenyUserAgent(allow...)
}

// WithAllowUserAgent adds user agent filtering to the endpoint.
//
// The allow argument is a regular expressions for the user-agent
// header to allow
//
// Any invalid regular expression will result in an error when creating the tunnel.
//
// https://ngrok.com/docs/http/user-agent-filter/
// ERR_NGROK_2090 for invalid allow/deny on connect.
// ERR_NGROK_3211 The server does not authorize requests from your user-agent
// ERR_NGROK_9022 Your account is not authorized to use user agent filtering.
func WithAllowUserAgent(allow ...string) HTTPEndpointOption {
	return &userAgentFilter{
		// slice of regex strings for allowed user agents
		Allow: allow,
	}
}

// WithDenyUserAgent adds user agent filtering to the endpoint.
//
// The deny argument is a regular expressions to
// deny, with allows taking precedence over denies.
//
// Any invalid regular expression will result in an error when creating the tunnel.
//
// https://ngrok.com/docs/http/user-agent-filter/
// ERR_NGROK_2090 for invalid allow/deny on connect.
// ERR_NGROK_3211 The server does not authorize requests from your user-agent
// ERR_NGROK_9022 Your account is not authorized to use user agent filtering.
func WithDenyUserAgent(deny ...string) HTTPEndpointOption {
	return &userAgentFilter{
		// slice of regex strings for denied user agents
		Deny: deny,
	}
}

func (b *userAgentFilter) toProtoConfig() *mw.MiddlewareConfiguration_UserAgentFilter {
	if b == nil {
		return nil
	}
	return &mw.MiddlewareConfiguration_UserAgentFilter{
		Allow: b.Allow,
		Deny:  b.Deny,
	}
}

func (b *userAgentFilter) merge(set userAgentFilter) *userAgentFilter {
	if b == nil {
		b = &userAgentFilter{}
	}

	b.Allow = append(b.Allow, set.Allow...)
	b.Deny = append(b.Deny, set.Deny...)

	return b
}

func (b userAgentFilter) ApplyHTTP(opts *httpOptions) {
	opts.UserAgentFilter = opts.UserAgentFilter.merge(b)
}
