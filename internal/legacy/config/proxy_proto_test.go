package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/v2/internal/tunnel/proto"

	_ "embed"
)

func testProxyProto[T tunnelConfigPrivate, O any, OT any](t *testing.T,
	makeOpts func(...OT) Tunnel,
	getProxyProto func(*O) proto.ProxyProto,
) {
	optsFunc := func(opts ...any) Tunnel {
		return makeOpts(assertSlice[OT](opts)...)
	}

	cases := testCases[T, O]{
		{
			name: "absent",
			opts: optsFunc(),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getProxyProto(opts)
				require.Equal(t, int(ProxyProtoNone), int(actual))
			},
		},
		{
			name: "with proxyproto",
			opts: optsFunc(WithProxyProto(ProxyProtoV2)),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getProxyProto(opts)
				require.Equal(t, int(ProxyProtoV2), int(actual))
			},
		},
	}

	cases.runAll(t)
}

func TestProxyProto(t *testing.T) {
	testProxyProto[*httpOptions](t, HTTPEndpoint, func(opts *proto.HTTPEndpoint) proto.ProxyProto {
		return opts.ProxyProto
	})
	testProxyProto[*tlsOptions](t, TLSEndpoint, func(opts *proto.TLSEndpoint) proto.ProxyProto {
		return opts.ProxyProto
	})
	testProxyProto[*tcpOptions](t, TCPEndpoint, func(opts *proto.TCPEndpoint) proto.ProxyProto {
		return opts.ProxyProto
	})
}
