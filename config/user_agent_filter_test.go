package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/mw"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func testUserAgentFilter[T tunnelConfigPrivate, O any, OT any](t *testing.T,
	makeOpts func(...OT) Tunnel,
	getUserAgentFilter func(*O) *mw.MiddlewareConfiguration_UserAgentFilter,
) {
	optsFunc := func(opts ...any) Tunnel {
		return makeOpts(assertSlice[OT](opts)...)
	}
	cases := testCases[T, O]{
		{
			name: "test empty",
			opts: optsFunc(),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getUserAgentFilter(opts)
				require.Nil(t, actual)
			},
		},
		{
			name: "test allow",
			opts: optsFunc(
				WithAllowUserAgent(`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`),
			),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getUserAgentFilter(opts)
				require.NotNil(t, actual)
				require.Nil(t, actual.Deny)
				require.Empty(t, actual.Deny)
				require.Equal(t, []string{`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`}, actual.Allow)
			},
		},
		{
			name: "test deny",
			opts: optsFunc(
				WithDenyUserAgent(`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`),
			),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getUserAgentFilter(opts)
				require.NotNil(t, actual)
				require.Nil(t, actual.Allow)
				require.Equal(t, []string{`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`}, actual.Deny)
			},
		},
		{
			name: "test allow and deny",
			opts: optsFunc(
				WithAllowUserAgent(`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`),
				WithDenyUserAgent(`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`),
			),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getUserAgentFilter(opts)
				require.NotNil(t, actual)
				require.Equal(t, []string{`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`}, actual.Allow)
				require.Equal(t, []string{`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`}, actual.Deny)
			},
		},
		{
			name: "test multiple",
			opts: optsFunc(
				WithAllowUserAgent(`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`),
				WithDenyUserAgent(`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`),
				WithAllowUserAgent(`(Pingdom2\.com_bot_version_)(\d+)\.(\d+)`),
				WithDenyUserAgent(`(Pingdom2\.com_bot_version_)(\d+)\.(\d+)`),
			),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getUserAgentFilter(opts)
				require.NotNil(t, actual)
				require.Equal(t, []string{`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`, `(Pingdom2\.com_bot_version_)(\d+)\.(\d+)`}, actual.Allow)
				require.Equal(t, []string{`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`, `(Pingdom2\.com_bot_version_)(\d+)\.(\d+)`}, actual.Deny)
			},
		},
	}

	cases.runAll(t)
}

func TestUserAgentFilter(t *testing.T) {
	testUserAgentFilter[*httpOptions](t, HTTPEndpoint,
		func(h *proto.HTTPEndpoint) *mw.MiddlewareConfiguration_UserAgentFilter {
			return h.UserAgentFilter
		})
}
