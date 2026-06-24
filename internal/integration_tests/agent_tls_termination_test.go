package integration_tests

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.ngrok.com/ngrok/v2"
	"golang.ngrok.com/ngrok/v2/internal/tlstest"
)

type tlsHandlerResult struct {
	message string
	err     error
}

// TestAgentTLSTerminationIntegration tests agent-based TLS termination with custom certificates
func TestAgentTLSTerminationIntegration(t *testing.T) {
	// Mark this test for parallel execution
	t.Parallel()

	// Generate test certificate
	cert, err := tlstest.CreateCertificate()
	if err != nil {
		t.Fatal(err)
	}

	// Setup agent
	agent, ctx := SetupAgent(t)

	// Create a TLS listener with TLS termination
	config := &tls.Config{
		Certificates: []tls.Certificate{*cert},
	}
	listener, err := agent.Listen(ctx, ngrok.WithURL("tls://"), ngrok.WithAgentTLSTermination(config))
	require.NoError(t, err, "Failed to create listener with TLS termination")

	// Verify the agent is configured with our TLS config
	require.NotNil(t, listener.AgentTLSTermination(), "AgentTLSTermination should return our TLS config")

	endpointURL := listener.URL().String()
	t.Logf("TLS endpoint URL: %s", endpointURL)

	// results carries each handled connection's outcome. It is never closed; the
	// handler goroutine is the only sender and stops when the listener is closed.
	// Buffered generously so retry/probe connections during endpoint propagation
	// can't block the accept loop.
	results := make(chan tlsHandlerResult, 256)
	stopHandler := make(chan struct{})
	handlerDone := make(chan struct{})

	// Accept connections in a loop so that every dial attempt made while the
	// endpoint is still propagating to ngrok's edge is serviced. The goroutine
	// must never touch testing.T: listener.Close() during cleanup unblocks
	// Accept with an error, which would otherwise race the test's completion.
	go func() {
		defer close(handlerDone)
		for {
			conn, err := listener.Accept()
			if err != nil {
				// Expected once the listener is closed during cleanup.
				return
			}

			func() {
				defer conn.Close()
				// Don't let a single stalled connection wedge the accept loop.
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

				message, err := serveTCPMessage(conn)
				select {
				case results <- tlsHandlerResult{message: message, err: err}:
				case <-stopHandler:
				}
			}()
		}
	}()

	// Cleanup: stop the handler, close the listener to unblock Accept, then wait
	// for the goroutine to exit so it can't outlive the test.
	defer func() {
		close(stopHandler)
		_ = listener.Close()
		select {
		case <-handlerDone:
		case <-time.After(5 * time.Second):
			t.Error("timed out waiting for TLS handler to exit")
		}
	}()

	// Parse the URL to get the host and port (default to 443 for TLS).
	u, err := url.Parse(endpointURL)
	require.NoError(t, err, "Failed to parse URL")
	host := u.Host
	if !strings.Contains(host, ":") {
		host = host + ":443"
	}

	clientConfig := &tls.Config{
		InsecureSkipVerify: true, // Skip verification for integration test
	}
	const expectedResponse = "Message received"

	// Retry the full TLS round trip until the endpoint propagates to the edge and
	// routes to our listener. Each attempt uses a unique payload so the handler
	// result can be correlated to the specific connection whose response we read.
	retryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var lastErr error
	for attempt := 1; ; attempt++ {
		if retryCtx.Err() != nil {
			require.Failf(t, "TLS endpoint did not become ready", "last error: %v", lastErr)
		}

		message := fmt.Sprintf("TLS test payload %d", attempt)
		respBody, err := tryTLSRoundTrip(retryCtx, host, clientConfig, message)
		if err == nil && respBody == expectedResponse {
			actualMessage, err := waitForHandlerResult(retryCtx, results, message)
			require.NoError(t, err, "handler should receive the message for the successful round trip")
			assert.Equal(t, message, actualMessage, "Message should match what was sent")
			assert.Equal(t, expectedResponse, respBody, "Response body should match expected")
			return
		}

		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("unexpected response body %q", respBody)
		}

		select {
		case <-time.After(250 * time.Millisecond):
		case <-retryCtx.Done():
		}
	}
}

// tryTLSRoundTrip performs a single TLS dial, writes message, and reads the
// response, all bounded by ctx and per-connection deadlines.
func tryTLSRoundTrip(ctx context.Context, host string, clientConfig *tls.Config, message string) (string, error) {
	dialer := tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 5 * time.Second},
		Config:    clientConfig,
	}

	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := conn.Write([]byte(message)); err != nil {
		return "", err
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}
	return string(buf[:n]), nil
}

// waitForHandlerResult waits for the handler result matching expectedMessage,
// skipping stale results from earlier retry attempts.
func waitForHandlerResult(ctx context.Context, results <-chan tlsHandlerResult, expectedMessage string) (string, error) {
	for {
		select {
		case result := <-results:
			if result.message != expectedMessage {
				continue // stale result from an earlier retry/probe attempt
			}
			return result.message, result.err
		case <-ctx.Done():
			return "", fmt.Errorf("timed out waiting for handler result for %q: %w", expectedMessage, ctx.Err())
		}
	}
}
