package config

import (
	"golang.ngrok.com/ngrok/internal/mw"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

// Configuration for webhook verification.
type webhookVerification struct {
	// The webhook provider
	Provider string
	// The secret for verifying webhooks from this provider.
	Secret proto.ObfuscatedString
}

func (wv *webhookVerification) toProtoConfig() *mw.MiddlewareConfiguration_WebhookVerification {
	if wv == nil {
		return nil
	}
	return &mw.MiddlewareConfiguration_WebhookVerification{
		Provider: wv.Provider,
		Secret:   wv.Secret.PlainText(),
	}
}

// WithWebhookVerification configures webhook verification for this edge.
//
// https://ngrok.com/docs/http/webhook-verification/
func WithWebhookVerification(provider string, secret string) HTTPEndpointOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.WebhookVerification = &webhookVerification{
			Provider: provider,
			Secret:   proto.ObfuscatedString(secret),
		}
	})
}
