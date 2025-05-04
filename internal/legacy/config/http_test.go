package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/v2/internal/tunnel/proto"
)

func TestHTTP(t *testing.T) {
	cases := testCases[*httpOptions, proto.HTTPEndpoint]{
		{
			name:         "empty",
			opts:         HTTPEndpoint(),
			expectProto:  ptr("https"),
			expectLabels: nil,
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				require.NotNil(t, opts)
			},
		},
	}

	cases.runAll(t)
}
