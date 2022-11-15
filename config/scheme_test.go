package config

import (
	"testing"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestScheme(t *testing.T) {
	cases := testCases[httpOptions, proto.HTTPEndpoint]{
		{
			name:        "default",
			opts:        HTTPEndpoint(),
			expectProto: stringPtr(string(SchemeHTTPS)),
		},
		{
			name:        "set https",
			opts:        HTTPEndpoint(WithScheme(SchemeHTTPS)),
			expectProto: stringPtr(string(SchemeHTTPS)),
		},
		{
			name:        "force http",
			opts:        HTTPEndpoint(WithScheme(SchemeHTTP)),
			expectProto: stringPtr(string(SchemeHTTP)),
		},
	}

	cases.runAll(t)
}
