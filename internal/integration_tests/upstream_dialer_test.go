package integration_tests

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.ngrok.com/ngrok/v2"
	"golang.ngrok.com/ngrok/v2/internal/testutil"
)

// erroringDialer implements the ngrok.Dialer interface for testing
// It returns an error and signals when it's called
type erroringDialer struct {
	syncPoint *testutil.SyncPoint // Synchronization using testutil
}

// newErroringDialer creates a new erroringDialer with synchronization
func newErroringDialer() *erroringDialer {
	return &erroringDialer{
		syncPoint: testutil.NewSyncPoint(),
	}
}

// Dial implements the ngrok.Dialer interface
func (d *erroringDialer) Dial(network, address string) (net.Conn, error) {
	// Signal that the dialer was called
	d.syncPoint.Signal()
	return nil, errors.New("custom dialer test error")
}

// DialContext implements the ngrok.Dialer interface
func (d *erroringDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	// Signal that the dialer was called
	d.syncPoint.Signal()
	return nil, errors.New("custom dialer test error")
}

// TestUpstreamDialer tests the WithUpstreamDialer functionality
func TestUpstreamDialer(t *testing.T) {
	// Mark this test for parallel execution
	t.Parallel()

	// Setup agent
	agent, ctx := SetupAgent(t)

	// Create a custom dialer that returns an error and has synchronization
	customDialer := newErroringDialer()

	// Use any arbitrary URL, the dialer will be called but will fail
	// We're only testing that our dialer gets invoked
	forwarder, err := agent.Forward(ctx,
		ngrok.WithUpstream("http://example.com", ngrok.WithUpstreamDialer(customDialer)),
	)
	require.NoError(t, err, "Failed to create forwarder")
	defer forwarder.Close()

	// Get the ngrok URL
	ngrokURL := forwarder.URL().String()
	t.Logf("Forwarder URL: %s", ngrokURL)

	// A freshly created endpoint takes time to propagate to ngrok's edge; until
	// it does, requests are rejected at the edge without ever reaching the
	// agent's dialer. Any request that does reach the agent invokes the custom
	// dialer, so a dialer call is both our readiness signal and the assertion
	// under test. Issue requests synchronously (each fully drained, so nothing
	// outlives the test) until the dialer fires.
	t.Log("Waiting for dialer to be called...")
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := client.Get(ngrokURL)
		if err == nil {
			_ = resp.Body.Close()
		}
		if customDialer.syncPoint.WaitTimeout(t, 250*time.Millisecond) {
			break
		}
		require.False(t, time.Now().After(deadline), "Timed out waiting for custom dialer to be called")
	}

	t.Log("Custom dialer was successfully called")
}
