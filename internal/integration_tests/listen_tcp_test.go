package integration_tests

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.ngrok.com/ngrok/v2"
	"golang.ngrok.com/ngrok/v2/internal/testutil"
)

// TestListenAndTCPConnection tests the basic functionality of listening for TCP connections
func TestListenAndTCPConnection(t *testing.T) {
	// Mark this test for parallel execution
	t.Parallel()
	// Setup agent
	agent, ctx, cancel := SetupAgent(t)
	defer cancel()

	// Setup TCP listener using the TCP scheme
	listener := SetupListener(t, agent, ctx, ngrok.WithURL("tcp://"))
	defer listener.Close()

	// Expected message
	expectedMessage := "Hello, TCP!"

	// Create synchronization points
	handlerReady := testutil.NewSyncPoint()
	requestComplete := testutil.NewSyncPoint()
	messageChan := make(chan string, 1)
	done := make(chan struct{})

	// Start a goroutine to handle a single connection
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

		// Handle TCP connection using utility function
		message, err := HandleTCPConnection(t, conn)
		assert.NoError(t, err, "Failed to handle TCP connection")
		if err != nil {
			return
		}
		messageChan <- message

		// Signal that the request is complete
		requestComplete.Signal()
	}()
	t.Cleanup(func() { <-done })

	// Wait for the handler to be ready to accept connections
	handlerReady.Wait(t)

	// Make TCP connection and send data
	// Extract host and port from the URL
	hostPort := listener.URL().Host
	t.Logf("Connecting to TCP endpoint: %s", hostPort)
	conn, err := MakeTCPConnection(t, ctx, hostPort)
	require.NoError(t, err, "Failed to connect to TCP endpoint")
	defer conn.Close()

	// Send test message
	_, err = conn.Write([]byte(expectedMessage))
	require.NoError(t, err, "Failed to send data")

	// Wait for the message to be received with timeout
	var actualMessage string
	select {
	case actualMessage = <-messageChan:
		// Received the message
	case <-time.After(500 * time.Millisecond):
		require.Fail(t, "Timed out waiting for message processing")
	}

	// Check that the message received matches what was sent
	assert.Equal(t, expectedMessage, actualMessage, "Message should match what was sent")

	// Read response
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	require.True(t, err == nil || err == io.EOF, "Failed to read response: %v", err)
	response := string(buf[:n])
	expectedResponse := "Message received"
	assert.Equal(t, expectedResponse, response, "Response should match expected")

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
