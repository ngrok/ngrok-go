package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestWebsocketTCPConversion(t *testing.T) {
	cases := testCases[*httpOptions, proto.HTTPEndpoint]{
		{
			name: "absent",
			opts: HTTPEndpoint(),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				require.Nil(t, opts.WebsocketTCPConverter)
			},
		},
		{
			name: "converted",
			opts: HTTPEndpoint(WithWebsocketTCPConversion()),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				require.NotNil(t, opts.WebsocketTCPConverter)
			},
		},
	}

	cases.runAll(t)
}
