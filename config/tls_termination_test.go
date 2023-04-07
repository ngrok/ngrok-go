package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestTLSTermination(t *testing.T) {
	cases := testCases[tlsOptions, proto.TLSEndpoint]{
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
			name: "managed",
			opts: TLSEndpoint(WithManagedTermination()),
			expectOpts: func(t *testing.T, opts *proto.TLSEndpoint) {
				actual := opts.TLSTermination
				require.NotNil(t, actual)
				require.NotNil(t, actual.Cert)
				require.NotNil(t, actual.Key)
			},
		},
	}

	cases.runAll(t)
}
