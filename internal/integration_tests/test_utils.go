package integration_tests

import (
	"bufio"
	"bytes"
	"context"
	"errors"
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

type httpStatusError struct {
	URL        string
	Status     string
	StatusCode int
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("post %s: http %s", e.URL, e.Status)
}

// SkipIfOffline skips the test if NGROK_TEST_ONLINE environment variable is not set
func SkipIfOffline(t *testing.T) {
	if os.Getenv("NGROK_TEST_ONLINE") == "" {
		t.Skip("Skipping online test because NGROK_TEST_ONLINE is not set")
	}
}

// SetupAgent creates and connects a new agent for testing
func SetupAgent(t *testing.T) (ngrok.Agent, context.Context) {
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
func SetupListener(ctx context.Context, tb testing.TB, agent ngrok.Agent, opts ...ngrok.EndpointOption) ngrok.EndpointListener {
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
		return resp, &httpStatusError{
			URL:        url,
			Status:     resp.Status,
			StatusCode: resp.StatusCode,
		}
	}
	return resp, nil
}

// MakeHTTPRequestWhenEndpointReady retries transient 404 responses that can
// occur while a freshly-created endpoint is propagating through ngrok's edge.
func MakeHTTPRequestWhenEndpointReady(ctx context.Context, tb testing.TB, url string, message string) (*http.Response, error) {
	tb.Helper()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for {
		resp, err := MakeHTTPRequest(ctx, tb, url, message)
		if err == nil {
			return resp, nil
		}
		if resp != nil {
			resp.Body.Close()
		}

		var statusErr *httpStatusError
		if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusNotFound {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("post %s: endpoint did not become ready: %w", url, err)
		}

		time.Sleep(250 * time.Millisecond)
	}
}

type httpListenerResult struct {
	message string
	err     error
}

func serveOneHTTPRequest(ctx context.Context, listener ngrok.EndpointListener) <-chan httpListenerResult {
	result := make(chan httpListenerResult, 1)

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			result <- httpListenerResult{err: fmt.Errorf("accept connection: %w", err)}
			return
		}
		defer conn.Close()

		message, err := handleHTTPRequest(conn)
		if err != nil {
			result <- httpListenerResult{err: fmt.Errorf("handle HTTP request: %w", err)}
			return
		}

		result <- httpListenerResult{message: message}
	}()
	return result
}

func MakeListenerHTTPRequest(ctx context.Context, tb testing.TB, listener ngrok.EndpointListener, message string) string {
	tb.Helper()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	result := serveOneHTTPRequest(ctx, listener)
	resp, err := MakeHTTPRequestWhenEndpointReady(ctx, tb, listener.URL().String(), message)
	require.NoError(tb, err)
	defer resp.Body.Close()

	select {
	case handled := <-result:
		require.NoError(tb, handled.err)
		return handled.message
	case <-time.After(5 * time.Second):
		tb.Fatal("timed out waiting for request processing")
		return ""
	}
}

// WaitForForwarderReady polls the forwarder endpoint until ngrok's edge is
// actually routing traffic to it, or the timeout elapses.
//
// A freshly-created endpoint takes time to propagate to ngrok's edge. Until it
// has, the edge responds with 404 even though the agent session is up, so a
// successful HTTP response alone does not mean the endpoint is ready - we must
// keep polling until the edge stops returning 404. Any other status (200 from a
// healthy upstream, 502 from an upstream that intentionally fails, etc.) means
// the edge is routing to the endpoint.
func WaitForForwarderReady(t *testing.T, url string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			status := resp.StatusCode
			resp.Body.Close()
			if status != http.StatusNotFound {
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
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
func HandleHTTPRequest(t testing.TB, conn net.Conn) (string, error) {
	t.Helper()
	return handleHTTPRequest(conn)
}

func handleHTTPRequest(conn net.Conn) (string, error) {
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
	return serveTCPMessage(conn)
}

// serveTCPMessage is the testing.T-free core of HandleTCPConnection so it can be
// called from background goroutines without risking testing.T use after the test
// has completed.
func serveTCPMessage(conn io.ReadWriteCloser) (string, error) {
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
