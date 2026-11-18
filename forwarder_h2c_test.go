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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newH2CUpstream starts a server that, like grpc-go, accepts only HTTP/2
// without TLS (h2c). It answers with the protocol the request arrived over.
func newH2CUpstream(t *testing.T) string {
	t.Helper()

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Proto)
	}))
	srv.Config.Protocols = new(http.Protocols)
	srv.Config.Protocols.SetUnencryptedHTTP2(true)
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}

func newH2CForwarder(t *testing.T) *endpointForwarder {
	t.Helper()

	e := newTestForwarder(t, newH2CUpstream(t))
	e.upstreamProtocol = "http2"
	e.endpointURL = url.URL{Scheme: "https", Host: "example.test"}
	return e
}

// With the http2 app protocol, the edge sends HTTP/2 with prior knowledge
// down the tunnel. gRPC needs that for trailers and full-duplex streams.
func TestForwarderHTTP2UpstreamAcceptsHTTP2FromEdge(t *testing.T) {
	edgeClient := startEdgeConn(t, newH2CForwarder(t))

	edge := &http.Transport{
		Protocols: new(http.Protocols),
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return edgeClient, nil
		},
	}
	edge.Protocols.SetUnencryptedHTTP2(true)
	t.Cleanup(edge.CloseIdleConnections)

	resp, err := (&http.Client{Transport: edge}).Get("http://example.test/")
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "HTTP/2.0", string(body))
}

// An HTTP/1.1 request from the edge must still reach an h2c upstream over
// HTTP/2: the upstream rejects HTTP/1.1.
func TestForwarderHTTP2UpstreamSpeaksH2CToHTTPUpstream(t *testing.T) {
	edgeClient := startEdgeConn(t, newH2CForwarder(t))

	_, err := io.WriteString(edgeClient, "GET / HTTP/1.1\r\nHost: example.test\r\n\r\n")
	require.NoError(t, err)

	resp, err := http.ReadResponse(bufio.NewReader(edgeClient), nil)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "HTTP/2.0", string(body))
}
