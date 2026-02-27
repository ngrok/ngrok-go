package integration_tests

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.ngrok.com/ngrok/v2"
	"golang.ngrok.com/ngrok/v2/internal/testcontext"
)

// TestEventHandlingIntegration tests that events are properly emitted and received
// during real connection workflows, focusing on connect and disconnect events.
func TestEventHandlingIntegration(t *testing.T) {
	// Skip if not running online tests
	SkipIfOffline(t)

	// Mark this test for parallel execution
	t.Parallel()

	// Create channels for capturing events
	connectEventCh := make(chan *ngrok.EventAgentConnectSucceeded, 1)
	disconnectEventCh := make(chan *ngrok.EventAgentDisconnected, 1)

	// Create a handler that categorizes events by type
	handler := func(evt ngrok.Event) {
		t.Logf("Received event: %s at %v", evt.EventType(), evt.Timestamp())

		switch e := evt.(type) {
		case *ngrok.EventAgentConnectSucceeded:
			select {
			case connectEventCh <- e:
				// Successfully sent event
			default:
				t.Logf("Channel full, dropping connect event")
			}
		case *ngrok.EventAgentDisconnected:
			select {
			case disconnectEventCh <- e:
				// Successfully sent event
			default:
				t.Logf("Channel full, dropping disconnect event")
			}
		default:
			// Log other events but don't process them
			t.Logf("Received other event type: %T", evt)
		}
	}

	// Get authentication token from environment
	authToken := os.Getenv("NGROK_AUTHTOKEN")
	require.NotEmpty(t, authToken, "NGROK_AUTHTOKEN environment variable is required but not set")

	// Create and connect an agent with the event handler
	agent, err := ngrok.NewAgent(
		ngrok.WithAuthtoken(authToken),
		ngrok.WithEventHandler(handler))
	require.NoError(t, err, "Failed to create agent")

	// Create a context with timeout for the test
	ctx := testcontext.ForTB(t)

	// Connect the agent (should trigger a connect event)
	t.Log("Connecting agent...")
	err = agent.Connect(ctx)
	require.NoError(t, err, "Failed to connect agent")

	// Ensure agent is disconnected at end of test
	defer func() {
		t.Log("Disconnecting agent...")
		_ = agent.Disconnect()
	}()

	// Wait for the connect event with timeout
	t.Log("Waiting for connect event...")
	var connectEvent *ngrok.EventAgentConnectSucceeded
	select {
	case connectEvent = <-connectEventCh:
		t.Log("Received connect event")
	case <-time.After(5 * time.Second):
		require.Fail(t, "Timeout waiting for connect event")
	}

	// Verify the connect event details
	// Note: We can't directly compare Session objects as they have different references
	// but may represent the same session
	assert.Equal(t, agent, connectEvent.Agent, "Connect event should have the correct agent")
	assert.False(t, connectEvent.Timestamp().IsZero(), "Connect event should have non-zero timestamp")

	// Explicitly disconnect the agent to trigger disconnect event
	t.Log("Disconnecting agent...")
	err = agent.Disconnect()
	assert.NoError(t, err, "Agent should disconnect without error")

	// Wait for the disconnect event with timeout
	t.Log("Waiting for disconnect event...")
	var disconnectEvent *ngrok.EventAgentDisconnected
	select {
	case disconnectEvent = <-disconnectEventCh:
		t.Log("Received disconnect event")
	case <-time.After(5 * time.Second):
		require.Fail(t, "Timeout waiting for disconnect event")
	}

	// Verify the disconnect event details
	// Note: We can't directly compare Session objects as they have different references
	assert.Equal(t, agent, disconnectEvent.Agent, "Disconnect event should have the correct agent")
	assert.False(t, disconnectEvent.Timestamp().IsZero(), "Disconnect event should have non-zero timestamp")
	// For client-triggered disconnect, the error may indicate "not reconnecting, session closed"
	// which is expected behavior
	t.Logf("Disconnect error: %v", disconnectEvent.Error)

	// Verify events are in chronological order
	assert.True(t, connectEvent.Timestamp().Before(disconnectEvent.Timestamp()),
		"Connect event (%v) should be before disconnect event (%v)",
		connectEvent.Timestamp(), disconnectEvent.Timestamp())
}
