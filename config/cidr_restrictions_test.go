package config

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/mw"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func mustParseCIDR(cidr string) *net.IPNet {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		panic("TEST BUG: invalid CIDR: " + cidr)
	}
	return ipnet
}

func testCIDRRestrictions[T tunnelConfigPrivate, O any, OT any](t *testing.T,
	makeOpts func(...OT) Tunnel,
	getRestrictions func(*O) *mw.MiddlewareConfiguration_IPRestriction,
) {
	optsFunc := func(opts ...any) Tunnel {
		return makeOpts(assertSlice[OT](opts)...)
	}
	cases := testCases[T, O]{
		{
			name: "allow string",
			opts: optsFunc(WithAllowCIDRString("127.0.0.0/8")),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getRestrictions(opts)
				require.NotNil(t, actual)
				require.Equal(t, []string{"127.0.0.0/8"}, actual.AllowCidrs)
			},
		},
		{
			name: "deny string",
			opts: optsFunc(WithDenyCIDRString("127.0.0.0/8")),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getRestrictions(opts)
				require.NotNil(t, actual)
				require.Equal(t, []string{"127.0.0.0/8"}, actual.DenyCidrs)
			},
		},
		{
			name: "allow ipnet",
			opts: optsFunc(WithAllowCIDR(mustParseCIDR("127.0.0.0/8"))),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getRestrictions(opts)
				require.NotNil(t, actual)
				require.Equal(t, []string{"127.0.0.0/8"}, actual.AllowCidrs)
			},
		},
		{
			name: "deny ipnet",
			opts: optsFunc(WithDenyCIDR(mustParseCIDR("127.0.0.0/8"))),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getRestrictions(opts)
				require.NotNil(t, actual)
				require.Equal(t, []string{"127.0.0.0/8"}, actual.DenyCidrs)
			},
		},
		{
			name: "allow multi",
			opts: optsFunc(
				WithAllowCIDRString("127.0.0.0/8"),
				WithAllowCIDRString("10.0.0.0/8"),
			),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getRestrictions(opts)
				require.NotNil(t, actual)
				require.ElementsMatch(t, []string{"127.0.0.0/8", "10.0.0.0/8"}, actual.AllowCidrs)
			},
		},
		{
			name: "deny multi",
			opts: optsFunc(
				WithDenyCIDRString("127.0.0.0/8"),
				WithDenyCIDRString("10.0.0.0/8"),
			),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getRestrictions(opts)
				require.NotNil(t, actual)
				require.ElementsMatch(t, []string{"127.0.0.0/8", "10.0.0.0/8"}, actual.DenyCidrs)
			},
		},
		{
			name: "allow and deny multi",
			opts: optsFunc(
				WithAllowCIDRString("127.0.0.0/8"),
				WithAllowCIDRString("10.0.0.0/8"),
				WithDenyCIDRString("192.0.0.0/8"),
				WithDenyCIDRString("172.0.0.0/8"),
			),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getRestrictions(opts)
				require.NotNil(t, actual)
				require.ElementsMatch(t, []string{"192.0.0.0/8", "172.0.0.0/8"}, actual.DenyCidrs)
				require.ElementsMatch(t, []string{"127.0.0.0/8", "10.0.0.0/8"}, actual.AllowCidrs)
			},
		},
		{
			name: "allow and deny multi ipnet",
			opts: optsFunc(
				WithAllowCIDR(mustParseCIDR("127.0.0.0/8")),
				WithAllowCIDR(mustParseCIDR("10.0.0.0/8")),
				WithDenyCIDR(mustParseCIDR("192.0.0.0/8")),
				WithDenyCIDR(mustParseCIDR("172.0.0.0/8")),
			),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getRestrictions(opts)
				require.NotNil(t, actual)
				require.ElementsMatch(t, []string{"192.0.0.0/8", "172.0.0.0/8"}, actual.DenyCidrs)
				require.ElementsMatch(t, []string{"127.0.0.0/8", "10.0.0.0/8"}, actual.AllowCidrs)
			},
		},
	}

	cases.runAll(t)
}

func TestCIDRRestrictions(t *testing.T) {
	testCIDRRestrictions[*httpOptions](t, HTTPEndpoint,
		func(h *proto.HTTPEndpoint) *mw.MiddlewareConfiguration_IPRestriction {
			return h.IPRestriction
		})
	testCIDRRestrictions[*tcpOptions](t, TCPEndpoint,
		func(h *proto.TCPEndpoint) *mw.MiddlewareConfiguration_IPRestriction {
			return h.IPRestriction
		})
	testCIDRRestrictions[*tlsOptions](t, TLSEndpoint,
		func(h *proto.TLSEndpoint) *mw.MiddlewareConfiguration_IPRestriction {
			return h.IPRestriction
		})
}
