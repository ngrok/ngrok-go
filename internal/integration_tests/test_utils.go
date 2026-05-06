package integration_tests

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.ngrok.com/ngrok/v2"
	"golang.ngrok.com/ngrok/v2/internal/testcontext"
)

// SkipIfOffline skips the test if NGROK_TEST_ONLINE environment variable is not set
func SkipIfOffline(t *testing.T) {
	if os.Getenv("NGROK_TEST_ONLINE") == "" {
		t.Skip("Skipping online test because NGROK_TEST_ONLINE is not set")
	}
}

// SetupAgent creates and connects a new agent for testing
func SetupAgent(t *testing.T) (*ngrok.Agent, context.Context) {
	// Skip if not running online tests
	SkipIfOffline(t)

	// Get authentication token from environment
	authToken := os.Getenv("NGROK_AUTHTOKEN")
	require.NotEmpty(t, authToken, "NGROK_AUTHTOKEN environment variable is required but not set")

	// Create a new agent for each test
	agent, err := ngrok.NewAgent(
		ngrok.WithAuthtoken(authToken),
	)
	require.NoError(t, err, "Failed to create agent")

	ctx := testcontext.ForTB(t)

	// Connect the agent using context.Background() so the session is not closed when
	// the test context is cancelled (which happens before cleanup functions run).
	// The session is explicitly closed by the agent.Disconnect() cleanup below.
	err = agent.Connect(context.Background())
	require.NoError(t, err, "Failed to connect agent")
	t.Cleanup(func() {
		if err := agent.Disconnect(); err != nil {
			t.Error("Agent disconnect:", err)
		}
	})

	return agent, ctx
}

// SetupListener sets up an ngrok listener with the specified options.
// SetupListener must be called from the goroutine running the test or benchmark.
// The returned listener will be closed when the test or benchmark function returns.
func SetupListener(ctx context.Context, tb testing.TB, agent *ngrok.Agent, opts ...ngrok.EndpointOption) *ngrok.EndpointListener {
	tb.Helper()

	// Create a listener endpoint
	listener, err := agent.Listen(ctx, opts...)
	if err != nil {
		tb.Fatal(err)
	}

	// Get the URL of the endpoint
	endpointURL := listener.URL().String()
	tb.Logf("Endpoint URL: %s", endpointURL)
	tb.Cleanup(func() {
		if err := listener.Close(); err != nil {
			tb.Error("Closing listener:", err)
		}
	})

	return listener
}

// MakeHTTPRequest makes an HTTP POST request to the specified URL with the given body.
// MakeHTTPRequest ensures that a new connection will be created for this request
// that will be closed before MakeHTTPRequest returns.
// If the HTTP response status code is not 200, MakeHTTPRequest will return an error.
// The body of the returned HTTP response does not need to be closed.
func MakeHTTPRequest(ctx context.Context, tb testing.TB, url string, message string) (*http.Response, error) {
	tb.Helper()

	// Create a client with a custom transport that doesn't reuse connections.
	client := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}
	defer client.CloseIdleConnections()

	// Make the request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(message))
	if err != nil {
		return nil, fmt.Errorf("post %s: %v", url, err)
	}

	tb.Logf("Making HTTP request to %s", url)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("post %s: %v", url, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	resp.Body = io.NopCloser(bytes.NewReader(data))
	if err != nil {
		return resp, fmt.Errorf("post %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return resp, fmt.Errorf("post %s: http %s", url, resp.Status)
	}
	return resp, nil
}

// WaitForForwarderReady polls the forwarder endpoint until it responds or times out
func WaitForForwarderReady(t *testing.T, url string) {
	client := &http.Client{Timeout: 100 * time.Millisecond}
	for start := time.Now(); time.Since(start) < 500*time.Millisecond; {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("Forwarder endpoint didn't become ready in expected time, continuing anyway")
}

// MakeTCPConnection establishes a TCP connection to the given address
func MakeTCPConnection(t *testing.T, ctx context.Context, address string) (io.ReadWriteCloser, error) {
	t.Helper()
	// Use a simple net.Dialer to connect to the TCP address
	dialer := &net.Dialer{
		Timeout: 500 * time.Millisecond,
	}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// HandleHTTPRequest processes an HTTP request from a connection and sends a response
func HandleHTTPRequest(t *testing.T, conn net.Conn) (string, error) {
	t.Helper()
	// Create a buffered reader for the connection
	reader := bufio.NewReader(conn)

	// Read the HTTP request
	request, err := http.ReadRequest(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read HTTP request: %w", err)
	}

	// Read the request body
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read request body: %w", err)
	}
	message := string(body)

	// Send a response
	response := http.Response{
		StatusCode: http.StatusOK,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
	}
	response.Header.Set("Content-Type", "text/plain")
	response.Body = io.NopCloser(strings.NewReader("Request received"))

	if err := response.Write(conn); err != nil {
		return message, fmt.Errorf("failed to write response: %w", err)
	}

	return message, nil
}

// HandleTCPConnection reads data from a TCP connection and sends a response
func HandleTCPConnection(t *testing.T, conn io.ReadWriteCloser) (string, error) {
	t.Helper()
	// Read data from the connection
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return "", fmt.Errorf("failed to read data: %w", err)
	}

	message := string(buf[:n])

	// Send a response
	response := "Message received"
	if _, err := conn.Write([]byte(response)); err != nil {
		return message, fmt.Errorf("failed to write response: %w", err)
	}

	return message, nil
}
