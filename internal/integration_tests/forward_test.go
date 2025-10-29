package integration_tests

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.ngrok.com/ngrok/v2"
	"golang.ngrok.com/ngrok/v2/internal/testutil"
)

// TestForward tests forwarding to a local web server
func TestForward(t *testing.T) {
	// Mark this test for parallel execution
	t.Parallel()
	// Setup agent
	agent, ctx, cancel := SetupAgent(t)
	defer cancel()
	defer func() { _ = agent.Disconnect() }()

	// Create a channel to signal when the server is ready
	serverReady := testutil.NewSyncPoint()

	// Start a local HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			assert.NoError(t, err, "Server failed to read body")
			http.Error(w, "Failed to read body", http.StatusInternalServerError)
			return
		}

		// Echo back what was received
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Received", string(body))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fmt.Appendf(nil, "Received: %s", string(body)))
	}))
	defer server.Close()

	// Signal that the server is ready
	serverReady.Signal()

	// Create a channel to signal when forwarding is ready
	forwarderReady := testutil.NewSyncPoint()

	// Forward to the local server
	forwarder, err := agent.Forward(ctx, ngrok.WithUpstream(server.URL))
	require.NoError(t, err, "Failed to create forwarder")
	defer forwarder.Close()

	// Get the ngrok URL
	ngrokURL := forwarder.URL().String()
	t.Logf("Forwarder URL: %s", ngrokURL)

	// Signal that forwarding is ready
	forwarderReady.Signal()

	// Send a request to the ngrok URL
	expectedMessage := "Hello from forward test!"
	resp := MakeHTTPRequest(t, ctx, ngrokURL, expectedMessage)
	defer resp.Body.Close()

	// Check the status code
	assert.Equal(t, http.StatusOK, resp.StatusCode, "HTTP status should be 200 OK")

	// Check the received header
	receivedHeader := resp.Header.Get("X-Received")
	assert.Equal(t, expectedMessage, receivedHeader, "Header X-Received should match the message sent")

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")

	// Verify the response body contains the expected message
	expectedResponsePrefix := "Received: " + expectedMessage
	assert.Contains(t, string(body), expectedResponsePrefix, "Response body should contain the expected message")
}
