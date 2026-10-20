package ngrok

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newEchoUpstream starts a raw TCP server that echoes every connection back to
// itself, and returns its address.
func newEchoUpstream(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close() //nolint:errcheck
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()

	return ln.Addr().String()
}

// startEdgeConn runs one inbound edge connection through handleConnection and
// returns the client side. The deadline turns a stalled forwarder into a test
// failure instead of a hang.
func startEdgeConn(t *testing.T, e *endpointForwarder) net.Conn {
	t.Helper()

	edgeClient, edgeServer := net.Pipe()
	t.Cleanup(func() { _ = edgeClient.Close() })
	require.NoError(t, edgeClient.SetDeadline(time.Now().Add(5*time.Second)))

	go e.handleConnection(context.Background(), edgeServer)

	return edgeClient
}

// A tcp or tls endpoint carries raw bytes. An http upstream URL must not make
// the forwarder parse them as HTTP.
func TestForwarderTCPEndpointWithHTTPUpstreamCopiesRawBytes(t *testing.T) {
	e := newTestForwarder(t, "http://"+newEchoUpstream(t))
	e.endpointURL = url.URL{Scheme: "tcp", Host: "1.tcp.ngrok.example:12345"}

	edgeClient := startEdgeConn(t, e)

	_, err := io.WriteString(edgeClient, "test")
	require.NoError(t, err)

	buf := make([]byte, 4)
	_, err = io.ReadFull(edgeClient, buf)
	require.NoError(t, err)
	assert.Equal(t, "test", string(buf))
}

// The reverse proxy drops hop-by-hop headers, so X-Hop never reaches the
// upstream. A raw copy would deliver it unchanged.
func TestForwarderHTTPEndpointWithHTTPUpstreamProxiesHTTP(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "x-hop="+r.Header.Get("X-Hop"))
	}))
	t.Cleanup(upstream.Close)

	e := newTestForwarder(t, upstream.URL)
	e.endpointURL = url.URL{Scheme: "https", Host: "example.test"}

	edgeClient := startEdgeConn(t, e)

	_, err := io.WriteString(edgeClient, "GET / HTTP/1.1\r\nHost: example.test\r\nConnection: X-Hop\r\nX-Hop: 1\r\n\r\n")
	require.NoError(t, err)

	resp, err := http.ReadResponse(bufio.NewReader(edgeClient), nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "x-hop=", string(body), "request did not pass through the reverse proxy")
}
