package config

import (
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/mw"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"

	_ "embed"
)

//go:embed testdata/ngrok.ca.crt
var ngrokCA []byte

func testMutualTLS[T tunnelConfigPrivate, O any, OT any](t *testing.T,
	makeOpts func(...OT) Tunnel,
	getMTLS func(*O) *mw.MiddlewareConfiguration_MutualTLS,
) {
	optsFunc := func(opts ...any) Tunnel {
		return makeOpts(assertSlice[OT](opts)...)
	}

	certDer, _ := pem.Decode(ngrokCA)
	cert, err := x509.ParseCertificate(certDer.Bytes)
	if err != nil {
		panic("failed to parse certificate: " + err.Error())
	}

	cases := testCases[T, O]{
		{
			name: "absent",
			opts: optsFunc(),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getMTLS(opts)
				require.Nil(t, actual)
			},
		},
		{
			name: "with mtls",
			opts: optsFunc(WithMutualTLSCA(cert)),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getMTLS(opts)
				require.NotNil(t, actual)
				require.Equal(t, ngrokCA, actual.MutualTlsCa)
			},
		},
	}

	cases.runAll(t)
}

func TestMutualTLS(t *testing.T) {
	testMutualTLS[*httpOptions](t, HTTPEndpoint, func(opts *proto.HTTPEndpoint) *mw.MiddlewareConfiguration_MutualTLS {
		return opts.MutualTLSCA
	})
	testMutualTLS[*tlsOptions](t, TLSEndpoint, func(opts *proto.TLSEndpoint) *mw.MiddlewareConfiguration_MutualTLS {
		return opts.MutualTLSAtEdge
	})
}
