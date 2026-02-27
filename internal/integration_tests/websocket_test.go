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
	"golang.org/x/net/websocket"
)

// TestWebSocketUpgrade tests that WebSocket upgrades work through the
// reverse proxy path.
func TestWebSocketUpgrade(t *testing.T) {
	t.Parallel()

	agent, ctx, cancel := SetupAgent(t)
	defer cancel()

	// Start an upstream WebSocket server.
	server := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		// Echo server: read messages and send them back
		_, _ = io.Copy(ws, ws)
	}))
	defer server.Close()

	forwarder, err := agent.Forward(ctx, ngrok.WithUpstream(server.URL))
	require.NoError(t, err, "Failed to create forwarder")
	defer forwarder.Close()

	ngrokURL := forwarder.URL().String()
	// Convert http:// to ws:// for WebSocket connection
	wsURL := "ws" + strings.TrimPrefix(ngrokURL, "http")
	t.Logf("WebSocket URL: %s", wsURL)

	t.Run("echo message", func(t *testing.T) {
		ws, err := websocket.Dial(wsURL, "", ngrokURL)
		require.NoError(t, err, "Failed to connect WebSocket")
		defer func() { _ = ws.Close() }()

		msg := "hello websocket"
		_, err = ws.Write([]byte(msg))
		require.NoError(t, err, "Failed to write to WebSocket")

		// Read the echo
		buf := make([]byte, 1024)
		n, err := ws.Read(buf)
		require.NoError(t, err, "Failed to read from WebSocket")

		assert.Equal(t, msg, string(buf[:n]), "WebSocket echo should match")
		t.Logf("Echoed: %s", string(buf[:n]))
	})

	t.Run("multiple messages", func(t *testing.T) {
		ws, err := websocket.Dial(wsURL, "", ngrokURL)
		require.NoError(t, err, "Failed to connect WebSocket")
		defer func() { _ = ws.Close() }()

		messages := []string{"first", "second", "third"}
		for _, msg := range messages {
			_, err = ws.Write([]byte(msg))
			require.NoError(t, err)

			buf := make([]byte, 1024)
			n, err := ws.Read(buf)
			require.NoError(t, err)

			assert.Equal(t, msg, string(buf[:n]))
		}
	})

	t.Run("regular HTTP still works", func(t *testing.T) {
		// Verify that non websocket requests will work on the same
		// forwarder, and not return a gateway error.
		transport := &http.Transport{DisableKeepAlives: true}
		client := &http.Client{Transport: transport}
		resp, err := client.Get(ngrokURL)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.NotEqual(t, http.StatusBadGateway, resp.StatusCode,
			"Should not get a gateway error")
	})
}
