package config

import "github.com/ngrok/ngrok-go/internal/pb_agent"

// Configuration for webhook verification.
type webhookVerification struct {
	// The webhook provider
	Provider string
	// The secret for verifying webhooks from this provider.
	Secret string
}

func (wv *webhookVerification) toProtoConfig() *pb_agent.MiddlewareConfiguration_WebhookVerification {
	if wv == nil {
		return nil
	}
	return &pb_agent.MiddlewareConfiguration_WebhookVerification{
		Provider: wv.Provider,
		Secret:   wv.Secret,
	}
}

// WithWebhookVerification configures webhook vericiation for this edge.
func WithWebhookVerification(provider string, secret string) HTTPEndpointOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.WebhookVerification = &webhookVerification{
			Provider: provider,
			Secret:   secret,
		}
	})
}
