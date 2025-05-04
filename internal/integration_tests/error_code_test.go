package integration_tests

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.ngrok.com/ngrok/v2"
)

// TestErrorCode tests that unique ngrok error codes are properly returned
func TestErrorCode(t *testing.T) {
	SkipIfOffline(t)
	t.Parallel()

	agent, ctx, cancel := SetupAgent(t)
	defer cancel()

	// Create an endpoint with an invalid character ('@') in its URL
	_, err := agent.Listen(ctx,
		ngrok.WithURL("https://invalid@domain.com"),
	)
	require.Error(t, err, "Expected an error when using invalid URL")

	var ngrokErr ngrok.Error
	require.True(t, errors.As(err, &ngrokErr), "Expected error to be of type ngrok.Error, got %T", err)

	errCode := ngrokErr.Code()
	require.Equal(t, "ERR_NGROK_9037", errCode, "Expected error code ERR_NGROK_9037 for invalid URL")
}
