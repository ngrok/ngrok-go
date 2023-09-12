package config

import (
	"golang.ngrok.com/ngrok/internal/pb"
)

// UserAgentFilter is a pair of strings slices that allow/deny traffic to an endpoint
type userAgentFilter struct {
	// slice of regex strings for allowed user agents
	Allow []string
	// slice of regex strings for denied user agents
	Deny []string
}

func (b *userAgentFilter) toProtoConfig() *pb.MiddlewareConfiguration_UserAgentFilter {
	if b == nil {
		return nil
	}
	return &pb.MiddlewareConfiguration_UserAgentFilter{
		Allow: b.Allow,
		Deny:  b.Deny,
	}
}

// WithAllowUserAgentFilter adds user agent filtering to the endpoint.
//
// The allow argument is a regular expressions for the user-agent
// header to allow
//
// Any invalid regular expression will result in an error when creating the tunnel.
//
// https://ngrok.com/docs/cloud-edge/modules/user-agent-filter
// ERR_NGROK_2090 for invalid allow/deny on connect.
// ERR_NGROK_3211 The server does not authorize requests from your user-agent
// ERR_NGROK_9022 Your account is not authorized to use user agent filtering.
func WithAllowUserAgentFilter(allow ...string) HTTPEndpointOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.UserAgentFilter = &userAgentFilter{
			// slice of regex strings for allowed user agents
			Allow: allow,
		}
	})
}

// WithDenyUserAgentFilter adds user agent filtering to the endpoint.
//
// The deny argument is a regular expressions to
// deny, with allows taking precedence over denies.
//
// Any invalid regular expression will result in an error when creating the tunnel.
//
// https://ngrok.com/docs/cloud-edge/modules/user-agent-filter
// ERR_NGROK_2090 for invalid allow/deny on connect.
// ERR_NGROK_3211 The server does not authorize requests from your user-agent
// ERR_NGROK_9022 Your account is not authorized to use user agent filtering.
func WithDenyUserAgentFilter(deny ...string) HTTPEndpointOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.UserAgentFilter = &userAgentFilter{
			// slice of regex strings for denied user agents
			Deny: deny,
		}
	})

}
func (base *userAgentFilter) merge(set userAgentFilter) *userAgentFilter {
	if base == nil {
		base = &userAgentFilter{}
	}

	base.Allow = append(base.Allow, set.Allow...)
	base.Deny = append(base.Deny, set.Deny...)

	return base
}

func (opt userAgentFilter) ApplyHTTP(opts *httpOptions) {
	opts.UserAgentFilter = opts.UserAgentFilter.merge(opt)
}
