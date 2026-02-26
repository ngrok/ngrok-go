package integration_tests

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.ngrok.com/ngrok/v2"
)

// TestEarlyResponseLargeUpload tests that when an upstream server rejects a
// large upload with an early error response (e.g., 413), the response is
// properly forwarded to the client instead of resulting in a gateway error.
func TestEarlyResponseLargeUpload(t *testing.T) {
	t.Parallel()

	agent, ctx, cancel := SetupAgent(t)
	defer cancel()
	defer func() { _ = agent.Disconnect() }()

	const maxBodySize = 1024

	// Start an upstream server that rejects large request bodies.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lr := io.LimitReader(r.Body, maxBodySize+1)
		body, _ := io.ReadAll(lr)

		if len(body) > maxBodySize {
			w.Header().Set("Connection", "close")
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			_, _ = w.Write([]byte("Payload too large"))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	forwarder, err := agent.Forward(ctx, ngrok.WithUpstream(server.URL))
	require.NoError(t, err, "Failed to create forwarder")
	defer forwarder.Close()

	ngrokURL := forwarder.URL().String()
	t.Logf("Forwarder URL: %s", ngrokURL)

	t.Run("small body succeeds", func(t *testing.T) {
		resp := MakeHTTPRequest(t, ctx, ngrokURL, "small payload")
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "OK", string(body))
	})

	t.Run("large body returns 413", func(t *testing.T) {
		// Send a body larger than the limit
		largeBody := strings.Repeat("x", 5*1024*1024)

		transport := &http.Transport{DisableKeepAlives: true}
		client := &http.Client{Transport: transport}

		req, err := http.NewRequestWithContext(ctx, "POST", ngrokURL, strings.NewReader(largeBody))
		require.NoError(t, err)

		t.Logf("Sending 5MB POST to %s", ngrokURL)
		resp, err := client.Do(req)
		require.NoError(t, err, "Request should complete without transport error")
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode,
			"Should receive 413 from upstream, not a gateway error")
		assert.Equal(t, "Payload too large", string(body))
		t.Logf("Response: %d %s", resp.StatusCode, string(body))
	})
}
