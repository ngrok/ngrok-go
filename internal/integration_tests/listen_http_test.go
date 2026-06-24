package integration_tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestListenAndHTTPRequest tests the basic functionality of listening for HTTP requests
func TestListenAndHTTPRequest(t *testing.T) {
	// Mark this test for parallel execution
	t.Parallel()
	// Setup agent
	agent, ctx := SetupAgent(t)

	// Setup listener
	listener := SetupListener(ctx, t, agent)

	// Expected message
	expectedMessage := "Hello, ngrok!"

	actualMessage := MakeListenerHTTPRequest(ctx, t, listener, expectedMessage)

	// Check that the message received matches what was sent
	assert.Equal(t, expectedMessage, actualMessage, "Message should match what was sent")
}
