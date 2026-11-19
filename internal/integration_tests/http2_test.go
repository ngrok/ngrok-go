package integration_tests

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.ngrok.com/ngrok/v2"
)

// TestUpstreamProtocolHTTP2 tests the WithUpstreamProtocol option
// to verify HTTP/2 connections to the upstream
func TestUpstreamProtocolHTTP2(t *testing.T) {
	t.Parallel()

	// Test 1: Without specifying protocol - should default to HTTP/1.1
	t.Run("Without protocol specified", func(t *testing.T) {
		t.Parallel()

		// Setup agent for this test
		agent, ctx := SetupAgent(t)

		// Set up a test HTTP/2 server
		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Report the protocol used
			protoVer := "HTTP/1.1"
			if r.ProtoMajor == 2 {
				protoVer = "HTTP/2.0"
			}

			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("X-Protocol-Version", protoVer)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(fmt.Appendf(nil, "Server used %s", protoVer))
		}))

		// Configure TLS with HTTP/2 support
		srv.TLS = &tls.Config{
			NextProtos: []string{"h2", "http/1.1"},
		}

		// Start the server with TLS and HTTP/2 enabled
		srv.StartTLS()
		defer srv.Close()

		// Create a forwarder without specifying protocol and skip cert verification
		tlsPool := x509.NewCertPool()
		tlsPool.AddCert(srv.Certificate())
		config := &tls.Config{
			RootCAs: tlsPool,
		}

		forwarder, err := agent.Forward(ctx,
			ngrok.WithUpstream(srv.URL, ngrok.WithUpstreamTLSClientConfig(config)),
		)
		require.NoError(t, err, "Failed to create forwarder")
		defer forwarder.Close()

		// Get the ngrok URL
		ngrokURL := forwarder.URL().String()
		t.Logf("Forwarder URL: %s", ngrokURL)
		WaitForForwarderReady(t, ngrokURL)

		WaitForForwarderReady(t, ngrokURL)

		// Send a request to the ngrok URL
		message := "Testing HTTP version"
		resp, err := MakeHTTPRequest(ctx, t, ngrokURL, message)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		// Read the response body
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err, "Failed to read response body")

		// Check protocol version - should be HTTP/1.1 when not specified
		protoHeader := resp.Header.Get("X-Protocol-Version")
		assert.Equal(t, "HTTP/1.1", protoHeader, "Protocol should be HTTP/1.1 when not specified")

		t.Logf("Response: %s", string(body))
	})

	// Test 2: With HTTP/2 protocol specified - should use HTTP/2
	t.Run("With HTTP/2 protocol specified", func(t *testing.T) {
		t.Parallel()

		// Setup agent for this test
		agent, ctx := SetupAgent(t)

		// Set up a test HTTP/2 server
		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Report the protocol used
			protoVer := "HTTP/1.1"
			if r.ProtoMajor == 2 {
				protoVer = "HTTP/2.0"
			}

			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("X-Protocol-Version", protoVer)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(fmt.Appendf(nil, "Server used %s", protoVer))
		}))

		// Configure TLS with HTTP/2 support
		srv.TLS = &tls.Config{
			NextProtos: []string{"h2", "http/1.1"},
		}

		// Start the server with TLS and HTTP/2 enabled
		srv.StartTLS()
		defer srv.Close()

		// Create a forwarder with HTTP/2 protocol and skip cert verification
		tlsPool := x509.NewCertPool()
		tlsPool.AddCert(srv.Certificate())
		config := &tls.Config{
			RootCAs: tlsPool,
		}

		forwarder, err := agent.Forward(ctx,
			ngrok.WithUpstream(srv.URL,
				ngrok.WithUpstreamProtocol("http2"),
				ngrok.WithUpstreamTLSClientConfig(config)),
		)
		require.NoError(t, err, "Failed to create forwarder")
		defer forwarder.Close()

		// Get the ngrok URL and wait for it to be ready
		ngrokURL := forwarder.URL().String()
		t.Logf("Forwarder URL: %s", ngrokURL)
		WaitForForwarderReady(t, ngrokURL)

		// Send a request to the ngrok URL
		message := "Testing HTTP2"
		resp, err := MakeHTTPRequest(ctx, t, ngrokURL, message)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		// Read the response body
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err, "Failed to read response body")

		// Check protocol version - should be HTTP/2.0 when specified
		protoHeader := resp.Header.Get("X-Protocol-Version")
		assert.Equal(t, "HTTP/2.0", protoHeader, "Protocol should be HTTP/2.0 when specified")

		t.Logf("Response: %s", string(body))
	})

	// Test 3: With HTTP/2 protocol to an unencrypted h2c-only HTTP upstream
	t.Run("With HTTP/2 protocol to h2c-only HTTP upstream", func(t *testing.T) {
		t.Parallel()

		agent, ctx := SetupAgent(t)

		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("X-Protocol-Version", r.Proto)
			w.Header().Set("Trailer", "X-Test-Trailer")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, "Server used %s", r.Proto)
			w.Header().Set("X-Test-Trailer", "ok")
		}))
		srv.Config.Protocols = new(http.Protocols)
		srv.Config.Protocols.SetUnencryptedHTTP2(true)
		srv.Start()
		defer srv.Close()

		forwarder, err := agent.Forward(ctx,
			ngrok.WithUpstream(srv.URL, ngrok.WithUpstreamProtocol("http2")),
		)
		require.NoError(t, err, "Failed to create forwarder")
		defer forwarder.Close()

		ngrokURL := forwarder.URL().String()
		t.Logf("Forwarder URL: %s", ngrokURL)
		WaitForForwarderReady(t, ngrokURL)

		transport := &http.Transport{ForceAttemptHTTP2: true}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ngrokURL, nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err, "Failed to read response body")

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, 2, resp.ProtoMajor, "Expected HTTP/2 response from ngrok edge")
		assert.Equal(t, "HTTP/2.0", resp.Header.Get("X-Protocol-Version"))
		assert.Equal(t, "ok", resp.Trailer.Get("X-Test-Trailer"))
		t.Logf("Response (%s): %s", resp.Proto, string(body))
	})

	// Test 4: With HTTP/1 protocol explicitly specified
	t.Run("With HTTP/1 protocol explicitly specified", func(t *testing.T) {
		t.Parallel()

		agent, ctx := SetupAgent(t)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("X-Protocol-Version", r.Proto)
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, "Server used %s", r.Proto)
		}))
		defer srv.Close()

		forwarder, err := agent.Forward(ctx,
			ngrok.WithUpstream(srv.URL, ngrok.WithUpstreamProtocol("http1")),
		)
		require.NoError(t, err, "Failed to create forwarder with http1 protocol")
		defer forwarder.Close()

		ngrokURL := forwarder.URL().String()
		t.Logf("Forwarder URL: %s", ngrokURL)
		WaitForForwarderReady(t, ngrokURL)

		resp, err := MakeHTTPRequest(ctx, t, ngrokURL, "Testing HTTP/1")
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err, "Failed to read response body")
		assert.Equal(t, "HTTP/1.1", resp.Header.Get("X-Protocol-Version"))
		t.Logf("Response: %s", string(body))
	})
}
