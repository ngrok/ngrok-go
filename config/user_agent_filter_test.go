package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/pb"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func testUserAgentFilter[T tunnelConfigPrivate, O any, OT any](t *testing.T,
	makeOpts func(...OT) Tunnel,
	getUserAgentFilter func(*O) *pb.MiddlewareConfiguration_UserAgentFilter,
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
				WithAllowUserAgentFilter(`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`),
			),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getUserAgentFilter(opts)
				require.NotNil(t, actual)
				require.Nil(t, actual.Deny)
				require.NotNil(t, actual.Allow)
				require.Len(t, actual.Allow, 1)
				require.Len(t, actual.Deny, 0)
				require.Contains(t, actual.Allow, `(Pingdom\.com_bot_version_)(\d+)\.(\d+)`)
			},
		},
		{
			name: "test deny",
			opts: optsFunc(
				WithDenyUserAgentFilter(`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`),
			),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getUserAgentFilter(opts)
				require.NotNil(t, actual)
				require.Nil(t, actual.Allow)
				require.Len(t, actual.Allow, 0)
				require.NotNil(t, actual.Deny)
				require.Len(t, actual.Deny, 1)
				require.Contains(t, actual.Deny, `(Pingdom\.com_bot_version_)(\d+)\.(\d+)`)
			},
		},
		{
			name: "test allow and deny",
			opts: optsFunc(
				WithAllowUserAgentFilter(`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`),
				WithDenyUserAgentFilter(`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`),
			),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getUserAgentFilter(opts)
				require.NotNil(t, actual)
				require.Len(t, actual.Allow, 1)
				require.Len(t, actual.Deny, 1)
				require.Contains(t, actual.Allow, `(Pingdom\.com_bot_version_)(\d+)\.(\d+)`)
				require.Contains(t, actual.Deny, `(Pingdom\.com_bot_version_)(\d+)\.(\d+)`)
			},
		},
		{
			name: "test multiple",
			opts: optsFunc(
				WithAllowUserAgentFilter(`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`),
				WithDenyUserAgentFilter(`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`),
				WithAllowUserAgentFilter(`(Pingdom2\.com_bot_version_)(\d+)\.(\d+)`),
				WithDenyUserAgentFilter(`(Pingdom2\.com_bot_version_)(\d+)\.(\d+)`),
			),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getUserAgentFilter(opts)
				require.NotNil(t, actual)
				require.Len(t, actual.Allow, 2)
				require.Len(t, actual.Deny, 2)
				require.Contains(t, actual.Allow, `(Pingdom\.com_bot_version_)(\d+)\.(\d+)`)
				require.Contains(t, actual.Deny, `(Pingdom\.com_bot_version_)(\d+)\.(\d+)`)
				require.Contains(t, actual.Allow, `(Pingdom2\.com_bot_version_)(\d+)\.(\d+)`)
				require.Contains(t, actual.Deny, `(Pingdom2\.com_bot_version_)(\d+)\.(\d+)`)
			},
		},
	}

	cases.runAll(t)
}

func TestUserAgentFilter(t *testing.T) {
	testUserAgentFilter[httpOptions](t, HTTPEndpoint,
		func(h *proto.HTTPEndpoint) *pb.MiddlewareConfiguration_UserAgentFilter {
			return h.UserAgentFilter
		})
}
