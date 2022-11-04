package config

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestHTTP(t *testing.T) {
	cases := testCases[httpOptions, proto.HTTPEndpoint]{
		{
			name:         "empty",
			opts:         HTTPEndpoint(),
			expectProto:  stringPtr("https"),
			expectLabels: labelPtr(nil),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				require.NotNil(t, opts)
			},
		},
	}

	cases.runAll(t)
}
