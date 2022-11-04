package config

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestOIDC(t *testing.T) {
	cases := testCases[httpOptions, proto.HTTPEndpoint]{
		{
			name: "absent",
			opts: HTTPEndpoint(),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				actual := opts.OAuth
				require.Nil(t, actual)
			},
		},
		{
			name: "simple",
			opts: HTTPEndpoint(WithOIDC("https://google.com", "foo", "bar")),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				actual := opts.OIDC
				require.NotNil(t, actual)
				require.Equal(t, "https://google.com", actual.IssuerURL)
				require.Equal(t, "foo", actual.ClientID)
				require.Equal(t, "bar", actual.ClientSecret)
			},
		},
		{
			name: "with options",
			opts: HTTPEndpoint(
				WithOIDC("google", "foo", "bar",
					WithOIDCScope("foo"),
					WithOIDCScope("bar", "baz"),
					WithAllowOIDCDomain("ngrok.com", "google.com"),
					WithAllowOIDCDomain("github.com"),
					WithAllowOIDCEmail("user1@gmail.com", "user2@gmail.com"),
					WithAllowOIDCEmail("user3@gmail.com"),
				),
			),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				actual := opts.OIDC
				require.NotNil(t, actual)
				require.Len(t, actual.Scopes, 3)
				require.Contains(t, actual.Scopes, "foo")
				require.Contains(t, actual.Scopes, "bar")
				require.Contains(t, actual.Scopes, "baz")
				require.Len(t, actual.AllowEmails, 3)
				require.Contains(t, actual.AllowEmails, "user1@gmail.com")
				require.Contains(t, actual.AllowEmails, "user2@gmail.com")
				require.Contains(t, actual.AllowEmails, "user3@gmail.com")
				require.Len(t, actual.AllowDomains, 3)
				require.Contains(t, actual.AllowDomains, "ngrok.com")
				require.Contains(t, actual.AllowDomains, "google.com")
				require.Contains(t, actual.AllowDomains, "github.com")
			},
		},
	}

	cases.runAll(t)
}
