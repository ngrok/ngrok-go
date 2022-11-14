package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func testDomain[T tunnelConfigPrivate, O any, OT any](t *testing.T,
	makeOpts func(...OT) Tunnel,
	getDomain func(*O) string,
) {
	optsFunc := func(opts ...any) Tunnel {
		return makeOpts(assertSlice[OT](opts)...)
	}

	cases := testCases[T, O]{
		{
			name: "absent",
			opts: optsFunc(),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getDomain(opts)
				require.Empty(t, actual)
			},
		},
		{
			name: "with domain",
			opts: optsFunc(WithDomain("foo.ngrok.io")),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getDomain(opts)
				require.NotEmpty(t, actual)
				require.Equal(t, "foo.ngrok.io", actual)
			},
		},
	}

	cases.runAll(t)
}

func TestDomain(t *testing.T) {
	testDomain[httpOptions](t, HTTPEndpoint, func(opts *proto.HTTPEndpoint) string {
		return opts.Hostname
	})
	testDomain[tlsOptions](t, TLSEndpoint, func(opts *proto.TLSEndpoint) string {
		return opts.Hostname
	})
}
