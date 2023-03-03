package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestWebhookVerification(t *testing.T) {
	cases := testCases[httpOptions, proto.HTTPEndpoint]{
		{
			name: "absent",
			opts: HTTPEndpoint(),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				actual := opts.WebhookVerification
				require.Nil(t, actual)
			},
		},
		{
			name: "single",
			opts: HTTPEndpoint(WithWebhookVerification("google", "domoarigato")),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				actual := opts.WebhookVerification
				require.NotNil(t, actual)
				require.Equal(t, "google", actual.Provider)
				require.Equal(t, "domoarigato", actual.Secret)
			},
		},
	}

	cases.runAll(t)
}
