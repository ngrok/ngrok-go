package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestUserAgentFilter(t *testing.T) {
	cases := testCases[httpOptions, proto.HTTPEndpoint]{
		{
			name: "nil",
			opts: HTTPEndpoint(),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				actual := opts.UserAgentFilter
				require.Nil(t, actual)
			},
		},
		{
			name: "testAllow",
			opts: HTTPEndpoint(WithAllowUserAgentFilter(`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`)),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				actual := opts.UserAgentFilter
				require.NotNil(t, actual)
				require.Equal(t, []string{`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`}, actual.Allow)

			},
		},
		{
			name: "testDeny",
			opts: HTTPEndpoint(WithDenyUserAgentFilter(`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`)),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				actual := opts.UserAgentFilter
				require.NotNil(t, actual)
				require.Equal(t, []string{`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`}, actual.Deny)

			},
		},
		{
			name: "testAllowAndDeny",
			opts: HTTPEndpoint(WithDenyUserAgentFilter(`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`), WithAllowUserAgentFilter(`(_bot_version_)(\d+)\.(\d+)`)),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				actual := opts.UserAgentFilter
				require.NotNil(t, actual)
				require.Equal(t, []string{`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`}, actual.Deny)
				require.Equal(t, []string{`(_bot_version_)(\d+)\.(\d+)`}, actual.Allow)

			},
		},
	}

	cases.runAll(t)
}
