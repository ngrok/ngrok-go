package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestOAuth(t *testing.T) {
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
			opts: HTTPEndpoint(WithOAuth("google")),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				actual := opts.OAuth
				require.NotNil(t, actual)
				require.Equal(t, "google", actual.Provider)
			},
		},
		{
			name: "with options",
			opts: HTTPEndpoint(
				WithOAuth("google",
					WithOAuthScope("foo"),
					WithOAuthScope("bar", "baz"),
					WithAllowOAuthDomain("ngrok.com", "google.com"),
					WithAllowOAuthDomain("github.com", "facebook.com"),
					WithAllowOAuthEmail("user1@gmail.com", "user2@gmail.com"),
					WithAllowOAuthEmail("user3@gmail.com"),
				),
			),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				actual := opts.OAuth
				require.NotNil(t, actual)
				require.Equal(t, "google", actual.Provider)
				require.Len(t, actual.Scopes, 3)
				require.Contains(t, actual.Scopes, "foo")
				require.Contains(t, actual.Scopes, "bar")
				require.Contains(t, actual.Scopes, "baz")
				require.Len(t, actual.AllowEmails, 3)
				require.Contains(t, actual.AllowEmails, "user1@gmail.com")
				require.Contains(t, actual.AllowEmails, "user2@gmail.com")
				require.Contains(t, actual.AllowEmails, "user3@gmail.com")
				require.Len(t, actual.AllowDomains, 4)
				require.Contains(t, actual.AllowDomains, "ngrok.com")
				require.Contains(t, actual.AllowDomains, "google.com")
				require.Contains(t, actual.AllowDomains, "facebook.com")
				require.Contains(t, actual.AllowDomains, "github.com")
			},
		},
	}

	cases.runAll(t)
}
