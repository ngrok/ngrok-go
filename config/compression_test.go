package config

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestCompression(t *testing.T) {
	cases := testCases[httpOptions, proto.HTTPEndpoint]{
		{
			name: "absent",
			opts: HTTPEndpoint(),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				require.Nil(t, opts.Compression)
			},
		},
		{
			name: "compressed",
			opts: HTTPEndpoint(WithCompression()),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				require.NotNil(t, opts.Compression)
			},
		},
	}

	cases.runAll(t)
}
