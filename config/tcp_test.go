package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestTCP(t *testing.T) {
	cases := testCases[tcpOptions, proto.TCPEndpoint]{
		{
			name:         "empty",
			opts:         TCPEndpoint(),
			expectProto:  stringPtr("tcp"),
			expectLabels: labelPtr(nil),
			expectOpts: func(t *testing.T, opts *proto.TCPEndpoint) {
				require.NotNil(t, opts)
				require.Empty(t, opts.Addr)
			},
		},
		{
			name:         "remote addr",
			opts:         TCPEndpoint(WithRemoteAddr("0.tcp.ngrok.io:1234")),
			expectProto:  stringPtr("tcp"),
			expectLabels: labelPtr(nil),
			expectOpts: func(t *testing.T, opts *proto.TCPEndpoint) {
				require.NotNil(t, opts)
				require.Equal(t, "0.tcp.ngrok.io:1234", opts.Addr)
			},
		},
	}

	cases.runAll(t)
}
