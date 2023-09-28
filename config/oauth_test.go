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
				require.ElementsMatch(t, []string{"foo", "bar", "baz"}, actual.Scopes)
				require.ElementsMatch(t, []string{"user1@gmail.com", "user2@gmail.com", "user3@gmail.com"}, actual.AllowEmails)
				require.ElementsMatch(t, []string{"ngrok.com", "google.com", "github.com", "facebook.com"}, actual.AllowDomains)
			},
		},
	}

	cases.runAll(t)
}
