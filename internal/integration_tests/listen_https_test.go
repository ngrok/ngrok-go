package integration_tests

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.ngrok.com/ngrok/v2"
	"golang.ngrok.com/ngrok/v2/internal/testutil"
)

// TestListenWithHTTPSURL tests using WithURL to specify an https URL
func TestListenWithHTTPSURL(t *testing.T) {
	// Mark this test for parallel execution
	t.Parallel()
	// Setup agent
	agent, ctx, cancel := SetupAgent(t)
	defer cancel()
	defer func() { _ = agent.Disconnect() }()

	// Setup listener with HTTPS URL
	httpsURL := "https://test-https.ngrok.io"
	listener := SetupListener(t, agent, ctx, ngrok.WithURL(httpsURL))
	defer listener.Close()

	// Verify the URL scheme is https
	assert.Equal(t, "https", listener.URL().Scheme, "URL scheme should be https")

	// Expected message
	expectedMessage := "HTTPS Test"

	// Create synchronization points
	handlerReady := testutil.NewSyncPoint()
	requestComplete := testutil.NewSyncPoint()
	messageChan := make(chan string, 1)
	done := make(chan struct{})

	// Start a goroutine to handle a single request
	go func() {
		defer close(done)

		// Accept a connection
		t.Log("Waiting for connection...")
		// Signal that we're ready to accept connections
		handlerReady.Signal()

		conn, err := listener.Accept()
		assert.NoError(t, err, "Failed to accept connection")
		if err != nil {
			return
		}
		defer conn.Close()
		t.Log("Connection accepted")

		// Handle the HTTP request using the utility function
		message, err := HandleHTTPRequest(t, conn)
		assert.NoError(t, err, "Failed to handle HTTP request")
		if err != nil {
			return
		}
		messageChan <- message

		// Signal that the request is complete
		requestComplete.Signal()
	}()

	// Wait for the handler to be ready to accept connections
	handlerReady.Wait(t)

	// Make HTTP request
	resp := MakeHTTPRequest(t, ctx, listener.URL().String(), expectedMessage)
	defer resp.Body.Close()

	// Wait for the message to be received with timeout
	var actualMessage string
	select {
	case actualMessage = <-messageChan:
		// Received the message
	case <-time.After(500 * time.Millisecond):
		require.Fail(t, "Timed out waiting for request processing")
	}

	// Check that the message received matches what was sent
	assert.Equal(t, expectedMessage, actualMessage, "Message should match what was sent")

	// Verify response status
	assert.Equal(t, http.StatusOK, resp.StatusCode, "HTTP status should be 200 OK")

	// Wait for the request to complete
	requestComplete.Wait(t)

	// Wait for the goroutine to finish with timeout
	select {
	case <-done:
		// Handler finished
	case <-time.After(500 * time.Millisecond):
		require.Fail(t, "Timed out waiting for handler to finish")
	}
}
