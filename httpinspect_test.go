package ngrok

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/websocket"
)

// upstreamConnCounter is an upstream test server that tracks how many
// connections are currently open. ConnState is installed before the server
// starts so the counter is race-free.
type upstreamConnCounter struct {
	*httptest.Server
	live atomic.Int64
}

func newCountingUpstream(t *testing.T, h http.Handler) *upstreamConnCounter {
	t.Helper()

	u := &upstreamConnCounter{}
	u.Server = httptest.NewUnstartedServer(h)
	u.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		switch state {
		case http.StateNew:
			u.live.Add(1)
		case http.StateClosed, http.StateHijacked:
			u.live.Add(-1)
		}
	}
	u.Start()
	t.Cleanup(u.Close)

	return u
}

// waitForLiveConns polls until the live connection count drops to within limit,
// and reports the final count.
func (u *upstreamConnCounter) waitForLiveConns(limit int64) int64 {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && u.live.Load() > limit {
		time.Sleep(25 * time.Millisecond)
	}
	return u.live.Load()
}

// newTestForwarder builds an endpointForwarder pointing at the given upstream
// without an agent or a session. emitConnectionEvent type-asserts on the agent
// field, and that assertion simply yields ok==false when it is nil, so the
// event path stays safe to exercise.
func newTestForwarder(t *testing.T, upstream string) *endpointForwarder {
	t.Helper()

	u, err := url.Parse(upstream)
	require.NoError(t, err)

	e := &endpointForwarder{}
	e.upstreamURL.Store(u)

	return e
}

// serveEdgeConn drives one inbound edge connection through httpServe: a single
// request, then the client hangs up. It blocks until httpServe returns.
func serveEdgeConn(t *testing.T, e *endpointForwarder) {
	t.Helper()

	edgeClient, edgeServer := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.httpServe(edgeServer)
	}()

	_, err := io.WriteString(edgeClient, "GET / HTTP/1.1\r\nHost: example.test\r\n\r\n")
	require.NoError(t, err)

	resp, err := http.ReadResponse(bufio.NewReader(edgeClient), nil)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.NoError(t, edgeClient.Close())

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("httpServe did not return after the edge connection closed")
	}
}

// Forwarding an HTTP upstream must not cost a permanent upstream socket per
// inbound edge connection. The assertion is deliberately "does not scale with
// N" rather than an exact count, so that it stays meaningful whether the
// transport is per-connection or shared and pooling idle sockets.
func TestHTTPServeDoesNotLeakUpstreamConns(t *testing.T) {
	upstream := newCountingUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))

	e := newTestForwarder(t, upstream.URL)

	const (
		n        = 25
		maxIdle  = 4
		maxExtra = 10
	)
	baseGoroutines := runtime.NumGoroutine()

	for range n {
		serveEdgeConn(t, e)
	}

	live := upstream.waitForLiveConns(maxIdle)
	t.Logf("%d edge connections left %d upstream connections open", n, live)
	assert.LessOrEqualf(t, live, int64(maxIdle),
		"upstream connections scale with the number of edge connections: %d left open after %d connections", live, n)

	deadline := time.Now().Add(3 * time.Second)
	delta := runtime.NumGoroutine() - baseGoroutines
	for time.Now().Before(deadline) && delta > maxExtra {
		time.Sleep(25 * time.Millisecond)
		delta = runtime.NumGoroutine() - baseGoroutines
	}
	assert.LessOrEqualf(t, delta, maxExtra,
		"goroutines scale with the number of edge connections: grew by %d after %d connections", delta, n)
}

// WebSocket upgrades hijack the connection, taking the upstream socket out of
// the idle pool. Tearing the transport down must not disturb them. This runs
// entirely offline: a loopback listener stands in for the ngrok edge.
func TestHTTPServeWebSocketUpgrade(t *testing.T) {
	upstream := newCountingUpstream(t, websocket.Handler(func(ws *websocket.Conn) {
		_, _ = io.Copy(ws, ws)
	}))

	e := newTestForwarder(t, upstream.URL)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close() //nolint:errcheck

	served := make(chan struct{})
	go func() {
		defer close(served)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		e.httpServe(conn)
	}()

	origin := "http://" + ln.Addr().String()
	ws, err := websocket.Dial("ws://"+ln.Addr().String(), "", origin)
	require.NoError(t, err, "websocket dial through httpServe")

	for _, msg := range []string{"hello", "second", "third"} {
		_, err := ws.Write([]byte(msg))
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := ws.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, msg, string(buf[:n]), "websocket echo")
	}

	require.NoError(t, ws.Close())

	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("httpServe did not return after the websocket closed")
	}

	assert.Zerof(t, upstream.waitForLiveConns(0),
		"upstream websocket connection was not released")
}

// A zero IdleConnTimeout means "no limit", which lets an idle upstream socket
// outlive any reasonable connection lifetime.
func TestBuildHTTPTransportSetsIdleConnTimeout(t *testing.T) {
	e := newTestForwarder(t, "http://127.0.0.1:1")
	assert.NotZero(t, e.buildHTTPTransport().IdleConnTimeout)
}
