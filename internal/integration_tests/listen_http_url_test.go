package integration_tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.ngrok.com/ngrok/v2"
)

// TestListenWithHTTPURL tests using WithURL to specify an http URL
func TestListenWithHTTPURL(t *testing.T) {
	// Mark this test for parallel execution
	t.Parallel()
	// Setup agent
	agent, ctx := SetupAgent(t)

	// Setup listener with HTTP URL
	httpURL := "http://test-http.ngrok.io"
	listener := SetupListener(ctx, t, agent, ngrok.WithURL(httpURL))

	// Verify the URL scheme is http
	assert.Equal(t, "http", listener.URL().Scheme, "URL scheme should be http")

	// Expected message
	expectedMessage := "HTTP Test"

	actualMessage := MakeListenerHTTPRequest(ctx, t, listener, expectedMessage)

	// Check that the message received matches what was sent
	assert.Equal(t, expectedMessage, actualMessage, "Message should match what was sent")
}
