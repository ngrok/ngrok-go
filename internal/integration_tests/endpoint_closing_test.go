package integration_tests

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEndpointClosingIntegration tests closing an endpoint while an agent session is live
// and verifies that cleanup works correctly (Done channel triggered, removed from endpoint list)
func TestEndpointClosingIntegration(t *testing.T) {
	// Mark this test for parallel execution
	t.Parallel()

	// Setup agent
	agent, ctx, cancel := SetupAgent(t)
	defer cancel()
	defer func() { _ = agent.Disconnect() }()

	// Create a listener endpoint
	listener, err := agent.Listen(ctx)
	require.NoError(t, err, "Failed to create listener")

	// Store the endpoint URL for verification
	endpointURL := listener.URL().String()
	t.Logf("Created endpoint: %s", endpointURL)

	// Verify the endpoint appears in the agent's endpoints list
	endpoints := agent.Endpoints()
	endpointFound := false
	for _, ep := range endpoints {
		if ep.URL().String() == endpointURL {
			endpointFound = true
			break
		}
	}
	require.True(t, endpointFound, "Endpoint should be found in agent's endpoints list")

	// Create a channel to monitor the endpoint's Done channel
	endpointClosed := make(chan struct{})
	go func() {
		<-listener.Done()
		close(endpointClosed)
	}()

	// Close the endpoint
	t.Log("Closing endpoint...")
	listener.Close()

	// Wait for the Done channel to be triggered with timeout
	select {
	case <-endpointClosed:
		t.Log("Endpoint Done channel was triggered successfully")
	case <-time.After(1 * time.Second):
		require.Fail(t, "Timeout waiting for endpoint Done channel to be triggered")
	}

	// Verify the endpoint is removed from the agent's endpoints list
	endpoints = agent.Endpoints()
	for _, ep := range endpoints {
		assert.NotEqual(t, endpointURL, ep.URL().String(), "Endpoint should not be found in agent's endpoints list after closing")
	}
}
