package config

import (
	"testing"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestTLS(t *testing.T) {
	cases := testCases[tlsOptions, proto.TLSEndpoint]{
		{
			name:         "basic",
			opts:         TLSEndpoint(),
			expectProto:  stringPtr("tls"),
			expectLabels: labelPtr(nil),
		},
	}

	cases.runAll(t)
}
