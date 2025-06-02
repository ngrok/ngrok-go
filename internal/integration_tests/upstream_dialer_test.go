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

// WaitForCall waits for the dialer to be called with a specified timeout
func (d *erroringDialer) WaitForCall(t testing.TB, timeout time.Duration) {
	success := d.syncPoint.WaitTimeout(t, timeout)
	require.True(t, success, "Timed out waiting for dialer to be called")
}

// TestUpstreamDialer tests the WithUpstreamDialer functionality
func TestUpstreamDialer(t *testing.T) {
	// Mark this test for parallel execution
	t.Parallel()

	// Setup agent
	agent, ctx, cancel := SetupAgent(t)
	defer cancel()
	defer func() { _ = agent.Disconnect() }()

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

	// Now make a request to trigger the dialer
	t.Logf("Making request to trigger upstream connection...")
	// The request will fail, but we'll ignore that since we expect it to fail
	// We're just triggering the ngrok service to use our dialer
	go func() {
		_, _ = http.Get(ngrokURL)
	}()

	// Wait for our dialer to be called with a timeout
	t.Log("Waiting for dialer to be called...")
	customDialer.WaitForCall(t, 3*time.Second)

	// If we got here, the test passed (WaitForCall would have failed if the dialer wasn't called)
	t.Log("Custom dialer was successfully called")
}
