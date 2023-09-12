package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/pb"
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
				require.Len(t, actual.Allow, 0)
				require.Len(t, actual.Deny, 0)
				require.Contains(t, actual.Allow, nil)
				require.Contains(t, actual.Deny, nil)
			},
		},
		{
			name: "test allow",
			opts: optsFunc(
				WithAllowUserAgentFilter(`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`),
			),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getUserAgentFilter(opts)
				require.Nil(t, actual)
				require.Nil(t, actual.Deny)
				require.NotNil(t, actual)
				require.Len(t, actual.Allow, 1)
				require.Len(t, actual.Deny, 0)
				require.Contains(t, actual.Allow, []string{`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`})
			},
		},
		{
			name: "test deny",
			opts: optsFunc(
				WithDenyUserAgentFilter(`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`),
			),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getUserAgentFilter(opts)
				require.Nil(t, actual)
				require.Nil(t, actual.Allow)
				require.NotNil(t, actual)
				require.Len(t, actual.Allow, 0)
				require.Len(t, actual.Deny, 1)
				require.Contains(t, actual.Deny, []string{`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`})
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
				require.Nil(t, actual)
				require.NotNil(t, actual)
				require.Len(t, actual.Allow, 1)
				require.Len(t, actual.Deny, 1)
				require.Contains(t, actual.Allow, []string{`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`})
				require.Contains(t, actual.Deny, []string{`(Pingdom\.com_bot_version_)(\d+)\.(\d+)`})
			},
		},
	}

	cases.runAll(t)
}
