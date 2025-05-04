package integration_tests

import (
	"crypto/tls"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.ngrok.com/ngrok/v2"
	"golang.ngrok.com/ngrok/v2/internal/testutil"
)

// TestAgentTLSTerminationIntegration tests agent-based TLS termination with custom certificates
func TestAgentTLSTerminationIntegration(t *testing.T) {
	// Mark this test for parallel execution
	t.Parallel()

	// Generate test certificate
	cert := CreateTestCertificate(t)

	// Setup agent
	agent, ctx, cancel := SetupAgent(t)
	defer cancel()
	defer func() { _ = agent.Disconnect() }()

	// Setup synchronization primitives
	handlerReady := testutil.NewSyncPoint()
	requestComplete := testutil.NewSyncPoint()
	messageChan := make(chan string, 1)
	done := make(chan struct{})

	// Create a TLS listener with TLS termination
	config := &tls.Config{
		Certificates: []tls.Certificate{*cert},
	}
	listener, err := agent.Listen(ctx, ngrok.WithURL("tls://"), ngrok.WithAgentTLSTermination(config))
	require.NoError(t, err, "Failed to create listener with TLS termination")
	defer listener.Close()

	// Verify the agent is configured with our TLS config
	require.NotNil(t, listener.AgentTLSTermination(), "AgentTLSTermination should return our TLS config")

	// Log the endpoint URL
	endpointURL := listener.URL().String()
	t.Logf("TLS endpoint URL: %s", endpointURL)

	// Start a goroutine to handle incoming connections
	go func() {
		defer close(done)

		// Signal that we're ready to accept connections
		handlerReady.Signal()

		// Accept a connection - this should be already TLS terminated
		conn, err := listener.Accept()
		assert.NoError(t, err, "Failed to accept connection")
		if err != nil {
			return
		}
		defer conn.Close()
		t.Log("Connection accepted")

		// Handle the TCP connection using our utility function
		message, err := HandleTCPConnection(t, conn)
		assert.NoError(t, err, "Failed to handle TCP connection")
		if err != nil {
			return
		}
		t.Logf("Received data from client: %q", message)
		messageChan <- message

		// Note: The HandleTCPConnection function has already sent a response

		// Signal that the request is complete
		requestComplete.Signal()
	}()

	// Wait for the handler to be ready to accept connections
	handlerReady.Wait(t)

	// Expected message
	expectedMessage := "TLS test payload"

	// Parse the URL to get the host and port
	u, err := url.Parse(endpointURL)
	require.NoError(t, err, "Failed to parse URL")

	// Make sure we have a port (default to 443 for HTTPS)
	host := u.Host
	if !strings.Contains(host, ":") {
		host = host + ":443"
	}

	// Connect to the endpoint using TLS
	clientConfig := &tls.Config{
		InsecureSkipVerify: true, // Skip verification for integration test
	}

	t.Logf("Connecting to TLS endpoint: %s (host: %s)", endpointURL, host)
	conn, err := tls.Dial("tcp", host, clientConfig)
	require.NoError(t, err, "Failed to connect to TLS endpoint")
	defer conn.Close()

	// Send the test message
	_, err = conn.Write([]byte(expectedMessage))
	require.NoError(t, err, "Failed to send data")

	// Read the response
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	require.NoError(t, err, "Failed to read response")
	respBody := string(buf[:n])

	// Wait for the message to be received with timeout
	var actualMessage string
	select {
	case actualMessage = <-messageChan:
		// Received the message
	case <-time.After(1 * time.Second):
		require.Fail(t, "Timed out waiting for request processing")
	}

	// Check that the message received matches what was sent
	assert.Equal(t, expectedMessage, actualMessage, "Message should match what was sent")

	// Wait for the request to complete
	requestComplete.Wait(t)

	// Verify the response body
	expectedResponse := "Message received"
	assert.Equal(t, expectedResponse, respBody, "Response body should match expected")

	// Wait for the goroutine to finish with timeout
	select {
	case <-done:
		// Handler finished
	case <-time.After(1 * time.Second):
		require.Fail(t, "Timed out waiting for handler to finish")
	}
}
