package integration_tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.ngrok.com/ngrok/v2"
)

// TestListenWithHTTPSURL tests using WithURL to specify an https URL
func TestListenWithHTTPSURL(t *testing.T) {
	// Mark this test for parallel execution
	t.Parallel()
	// Setup agent
	agent, ctx := SetupAgent(t)

	// Setup listener with HTTPS URL
	httpsURL := "https://test-https.ngrok.io"
	listener := SetupListener(ctx, t, agent, ngrok.WithURL(httpsURL))

	// Verify the URL scheme is https
	assert.Equal(t, "https", listener.URL().Scheme, "URL scheme should be https")

	// Expected message
	expectedMessage := "HTTPS Test"

	actualMessage := MakeListenerHTTPRequest(ctx, t, listener, expectedMessage)

	// Check that the message received matches what was sent
	assert.Equal(t, expectedMessage, actualMessage, "Message should match what was sent")
}
