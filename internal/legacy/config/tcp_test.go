package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/v2/internal/tunnel/proto"
)

func TestTCP(t *testing.T) {
	cases := testCases[*tcpOptions, proto.TCPEndpoint]{
		{
			name:         "empty",
			opts:         TCPEndpoint(),
			expectProto:  ptr("tcp"),
			expectLabels: nil,
			expectOpts: func(t *testing.T, opts *proto.TCPEndpoint) {
				require.NotNil(t, opts)
				require.Empty(t, opts.Addr)
			},
		},
		{
			name:         "remote addr",
			opts:         TCPEndpoint(WithURL("tcp://0.tcp.ngrok.io:1234")),
			expectProto:  ptr("tcp"),
			expectLabels: nil,
			expectOpts: func(t *testing.T, opts *proto.TCPEndpoint) {
				require.NotNil(t, opts)
				require.Equal(t, "tcp://0.tcp.ngrok.io:1234", opts.URL)
			},
		},
	}

	cases.runAll(t)
}
