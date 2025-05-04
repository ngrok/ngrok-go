package integration_tests

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.ngrok.com/ngrok/v2"
	"golang.ngrok.com/ngrok/v2/internal/testutil"
)

// TestListenWithURLAndPooling tests load balancing across two endpoints with the same URL
func TestListenWithURLAndPooling(t *testing.T) {
	// Mark this test for parallel execution
	t.Parallel()

	// Setup agent
	agent, ctx, cancel := SetupAgent(t)
	defer cancel()
	defer func() { _ = agent.Disconnect() }()

	// Common URL for both endpoints - IMPORTANT: the exact same string must be used for both listeners
	sharedURL := "https://test-lb.ngrok.io"

	// Create sync points for coordination
	listenersReady := testutil.NewSyncPoint()
	requestedFinished := testutil.NewSyncPoint()

	// Setup first listener with pooling enabled
	listener1 := SetupListener(t, agent, ctx, ngrok.WithURL(sharedURL), ngrok.WithPoolingEnabled(true))
	defer listener1.Close()

	// Setup second listener with the same URL and pooling enabled
	listener2 := SetupListener(t, agent, ctx, ngrok.WithURL(sharedURL), ngrok.WithPoolingEnabled(true))
	defer listener2.Close()

	// Log URLs for debugging
	t.Logf("Listener1 URL: %s, Pooling: %v", listener1.URL().String(), listener1.PoolingEnabled())
	t.Logf("Listener2 URL: %s, Pooling: %v", listener2.URL().String(), listener2.PoolingEnabled())

	// Verify both have the same URL - but note that load balancing can work even with different returned URLs
	// since the WithURL value is what's important for ngrok's backend pooling, not the returned URL
	if listener1.URL().String() != listener2.URL().String() {
		t.Logf("Warning: URLs don't match exactly, but load balancing may still work: %s and %s",
			listener1.URL().String(), listener2.URL().String())
	}

	// Track which endpoint receives each request
	var (
		mu                sync.Mutex
		endpoint1Requests int
		endpoint2Requests int
		wg                sync.WaitGroup
		endpoint1Ready    = testutil.NewSyncPoint()
		endpoint2Ready    = testutil.NewSyncPoint()
		processingDone    = make(chan struct{})
		testFinished      = make(chan struct{}) // Signal that test is finished so goroutines can ignore errors
	)

	// Start handlers for both listeners
	// Handler for first listener
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(processingDone)

		// Signal that we're ready to accept connections
		endpoint1Ready.Signal()

		for i := 0; i < 5; i++ { // Handle up to 5 connections
			conn, err := listener1.Accept()
			if err != nil {
				// Check if test is already finished before reporting errors
				select {
				case <-testFinished:
					// Test is done, just return silently
					return
				default:
					// Test still running, check the error type
					if strings.Contains(err.Error(), "listener closed") {
						t.Log("Listener1 closed")
						return
					}
					t.Logf("Listener1 accept error: %v", err)
					return
				}
			}

			// Process in a new goroutine
			go func(conn net.Conn) {
				defer conn.Close()

				// Track this request for listener1
				mu.Lock()
				endpoint1Requests++
				mu.Unlock()

				// Handle the HTTP request
				request, err := http.ReadRequest(bufio.NewReader(conn))
				if err != nil {
					t.Errorf("Failed to read HTTP request: %v", err)
					return
				}

				// Read the request body
				_, err = io.ReadAll(request.Body)
				if err != nil {
					t.Errorf("Failed to read request body: %v", err)
					return
				}

				// Send a response with endpoint identifier
				response := http.Response{
					StatusCode: http.StatusOK,
					Proto:      "HTTP/1.1",
					ProtoMajor: 1,
					ProtoMinor: 1,
					Header:     make(http.Header),
				}
				response.Header.Set("Content-Type", "text/plain")
				response.Header.Set("X-Endpoint", "endpoint1")
				response.Body = io.NopCloser(strings.NewReader("Response from endpoint 1"))

				if err := response.Write(conn); err != nil {
					t.Errorf("Failed to write response: %v", err)
				}
			}(conn)
		}
	}()

	// Handler for second listener
	wg.Add(1)
	go func() {
		defer wg.Done()

		// Signal that we're ready to accept connections
		endpoint2Ready.Signal()

		for i := 0; i < 5; i++ { // Handle up to 5 connections
			conn, err := listener2.Accept()
			if err != nil {
				// Check if test is already finished before reporting errors
				select {
				case <-testFinished:
					// Test is done, just return silently
					return
				default:
					// Test still running, check the error type
					if strings.Contains(err.Error(), "listener closed") {
						t.Log("Listener2 closed")
						return
					}
					t.Logf("Listener2 accept error: %v", err)
					return
				}
			}

			// Process in a new goroutine
			go func(conn net.Conn) {
				defer conn.Close()

				// Track this request for listener2
				mu.Lock()
				endpoint2Requests++
				mu.Unlock()

				// Handle the HTTP request
				request, err := http.ReadRequest(bufio.NewReader(conn))
				if err != nil {
					t.Errorf("Failed to read HTTP request: %v", err)
					return
				}

				// Read the request body
				_, err = io.ReadAll(request.Body)
				if err != nil {
					t.Errorf("Failed to read request body: %v", err)
					return
				}

				// Send a response with endpoint identifier
				response := http.Response{
					StatusCode: http.StatusOK,
					Proto:      "HTTP/1.1",
					ProtoMajor: 1,
					ProtoMinor: 1,
					Header:     make(http.Header),
				}
				response.Header.Set("Content-Type", "text/plain")
				response.Header.Set("X-Endpoint", "endpoint2")
				response.Body = io.NopCloser(strings.NewReader("Response from endpoint 2"))

				if err := response.Write(conn); err != nil {
					t.Errorf("Failed to write response: %v", err)
				}
			}(conn)
		}
	}()

	// Wait for both endpoints to be ready to accept connections
	endpoint1Ready.Wait(t)
	endpoint2Ready.Wait(t)

	// Signal that listeners are ready
	listenersReady.Signal()

	// Create a channel to signal when both endpoints have received at least one request
	bothEndpointsHit := make(chan struct{})
	maxRequests := 20 // Safety limit to prevent infinite loop
	requestCount := 0
	url := listener1.URL().String() // Both listeners have the same URL

	// Start a goroutine to monitor when both endpoints have been hit
	go func() {
		for {
			mu.Lock()
			ep1Hit := endpoint1Requests > 0
			ep2Hit := endpoint2Requests > 0
			mu.Unlock()

			if ep1Hit && ep2Hit {
				close(bothEndpointsHit)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()

	// Send requests until both endpoints have been hit or we reach max requests
	for requestCount < maxRequests {
		select {
		case <-bothEndpointsHit:
			// Both endpoints have received at least one request
			t.Log("Both endpoints have received requests")
			goto testComplete
		default:
			// Send another request
			requestCount++
			message := fmt.Sprintf("Request %d", requestCount)

			// Make HTTP request with a new connection each time
			resp := MakeHTTPRequest(t, ctx, url, message)

			// Read the response to see which endpoint responded
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Errorf("Failed to read response body: %v", err)
			}
			t.Logf("Response %d: %s, Header: %s", requestCount, string(body), resp.Header.Get("X-Endpoint"))

			resp.Body.Close()
			time.Sleep(50 * time.Millisecond) // Small delay between requests
		}
	}

	// If we reach here, we hit the max requests without both endpoints receiving traffic
	require.Fail(t, fmt.Sprintf("Sent %d requests but both endpoints weren't hit", maxRequests))

testComplete:

	// Signal that all requests are finished
	requestedFinished.Signal()

	// Signal that we're about to close listeners - this will help handle error reporting
	doneProcessing := make(chan struct{})
	go func() {
		// Close the listeners to stop the handler goroutines
		listener1.Close()
		listener2.Close()
		close(doneProcessing)
	}()

	// Wait for processing to finish with timeout
	select {
	case <-processingDone:
		// Processing completed
	case <-doneProcessing:
		// Listeners closed
	case <-time.After(500 * time.Millisecond):
		t.Log("Processing timeout - continuing with verification")
	}

	// Verify that both endpoints received requests
	mu.Lock()
	endpoint1Count := endpoint1Requests
	endpoint2Count := endpoint2Requests
	mu.Unlock()

	t.Logf("Endpoint 1 received %d requests", endpoint1Count)
	t.Logf("Endpoint 2 received %d requests", endpoint2Count)

	// Both endpoints should have received at least one request
	assert.NotZero(t, endpoint1Count, "Endpoint 1 should receive at least one request")
	assert.NotZero(t, endpoint2Count, "Endpoint 2 should receive at least one request")

	// Wait for handlers to finish with timeout
	c := make(chan struct{})
	go func() {
		wg.Wait()
		close(c)
	}()

	select {
	case <-c:
		// Handlers finished
	case <-time.After(500 * time.Millisecond):
		t.Log("Timed out waiting for handlers to finish")
	}

	// Signal that the test is completely finished so goroutines can clean up
	close(testFinished)
}
