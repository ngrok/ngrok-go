package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestTLSTermination(t *testing.T) {
	cases := testCases[*tlsOptions, proto.TLSEndpoint]{
		{
			name: "absent",
			opts: TLSEndpoint(),
			expectOpts: func(t *testing.T, opts *proto.TLSEndpoint) {
				require.Nil(t, opts.TLSTermination)
			},
		},
		{
			name: "with termination",
			opts: TLSEndpoint(WithTermination([]byte("cert"), []byte("key"))),
			expectOpts: func(t *testing.T, opts *proto.TLSEndpoint) {
				actual := opts.TLSTermination
				require.NotNil(t, actual)
				require.Equal(t, []byte("cert"), actual.Cert)
				require.Equal(t, []byte("key"), actual.Key)
			},
		},
		{
			name: "with new termination",
			opts: TLSEndpoint(WithTLSTermination()),
			expectOpts: func(t *testing.T, opts *proto.TLSEndpoint) {
				actual := opts.TLSTermination
				require.NotNil(t, actual)
				require.Equal(t, []byte{}, actual.Cert)
				require.Equal(t, []byte{}, actual.Key)
			},
		},
		{
			name: "with new nil termination",
			opts: TLSEndpoint(WithTLSTermination(WithTLSTerminationKeyPair(nil, nil))),
			expectOpts: func(t *testing.T, opts *proto.TLSEndpoint) {
				actual := opts.TLSTermination
				require.NotNil(t, actual)
				require.Equal(t, []byte(nil), actual.Cert)
				require.Equal(t, []byte(nil), actual.Key)
			},
		},
		{
			name: "with new custom termination",
			opts: TLSEndpoint(WithTLSTermination(WithTLSTerminationKeyPair([]byte("cert"), []byte("key")))),
			expectOpts: func(t *testing.T, opts *proto.TLSEndpoint) {
				actual := opts.TLSTermination
				require.NotNil(t, actual)
				require.Equal(t, []byte("cert"), actual.Cert)
				require.Equal(t, []byte("key"), actual.Key)
			},
		},
	}

	cases.runAll(t)
}
