package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestOIDC(t *testing.T) {
	cases := testCases[*httpOptions, proto.HTTPEndpoint]{
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
				require.Equal(t, "https://google.com", actual.IssuerUrl)
				require.Equal(t, "foo", actual.ClientId)
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
				require.ElementsMatch(t, []string{"foo", "bar", "baz"}, actual.Scopes)
				require.ElementsMatch(t, []string{"user1@gmail.com", "user2@gmail.com", "user3@gmail.com"}, actual.AllowEmails)
				require.ElementsMatch(t, []string{"ngrok.com", "google.com", "github.com"}, actual.AllowDomains)
			},
		},
	}

	cases.runAll(t)
}
