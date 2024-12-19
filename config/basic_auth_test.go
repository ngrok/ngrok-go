package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/mw"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestBasicAuth(t *testing.T) {
	cases := testCases[*httpOptions, proto.HTTPEndpoint]{
		{
			name: "single",
			opts: HTTPEndpoint(WithBasicAuth("foo", "bar")),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				require.NotNil(t, opts.BasicAuth)
				require.Len(t, opts.BasicAuth.Credentials, 1)
				require.Contains(t, opts.BasicAuth.Credentials, &mw.MiddlewareConfiguration_BasicAuthCredential{
					Username:          "foo",
					CleartextPassword: "bar",
				})
			},
		},
		{
			name: "multiple",
			opts: HTTPEndpoint(
				WithBasicAuth("foo", "bar"),
				WithBasicAuth("spam", "eggs"),
			),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				require.NotNil(t, opts.BasicAuth)
				require.Len(t, opts.BasicAuth.Credentials, 2)
				require.Contains(t, opts.BasicAuth.Credentials, &mw.MiddlewareConfiguration_BasicAuthCredential{
					Username:          "foo",
					CleartextPassword: "bar",
				})
				require.Contains(t, opts.BasicAuth.Credentials, &mw.MiddlewareConfiguration_BasicAuthCredential{
					Username:          "spam",
					CleartextPassword: "eggs",
				})
			},
		},
	}

	cases.runAll(t)
}
