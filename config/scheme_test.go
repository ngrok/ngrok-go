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
			expectProto: ptr(string(SchemeHTTPS)),
		},
		{
			name:        "set https",
			opts:        HTTPEndpoint(WithScheme(SchemeHTTPS)),
			expectProto: ptr(string(SchemeHTTPS)),
		},
		{
			name:        "force http",
			opts:        HTTPEndpoint(WithScheme(SchemeHTTP)),
			expectProto: ptr(string(SchemeHTTP)),
		},
	}

	cases.runAll(t)
}
