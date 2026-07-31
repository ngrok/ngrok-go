package ngrok

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeForwarder builds a minimal endpointForwarder pointing at the given upstream
// URL — no agent, no authtoken, no ngrok session needed.
// emitConnectionEvent's type assertion on a nil agent simply yields ok==false,
// so it is safe to leave agent unset.
func makeForwarder(t *testing.T, upstreamURL string) *endpointForwarder {
	t.Helper()
	u, err := url.Parse(upstreamURL)
	require.NoError(t, err)
	ef := &endpointForwarder{}
	ef.upstreamURL.Store(u)
	return ef
}

// TestHTTPServeNoFDLeak verifies that after httpServe returns, all upstream
// connections (including idle keep-alive sockets) are closed and the goroutine
// count returns to its baseline — i.e., neither Leak 1 nor Leak 2 is present.
func TestHTTPServeNoFDLeak(t *testing.T) {
	// Count active upstream connections via ConnState.
	var activeUpstream atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))
	upstream.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		switch state {
		case http.StateNew:
			activeUpstream.Add(1)
		case http.StateClosed, http.StateHijacked:
			activeUpstream.Add(-1)
		}
	}
	defer upstream.Close()

	baseGoroutines := runtime.NumGoroutine()

	// Run N sequential edge connections, each sending one request, to
	// ensure the leak compounds and is detectable.
	const N = 5
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ef := makeForwarder(t, upstream.URL)

			// Create a net.Pipe to act as the inbound edge connection.
			edgeClient, edgeServer := net.Pipe()
			defer edgeClient.Close() //nolint:errcheck

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// httpServe blocks until the connection is done; run in goroutine.
			done := make(chan struct{})
			go func() {
				defer close(done)
				ef.httpServe(ctx, edgeServer)
			}()

			// Send a minimal HTTP/1.1 request over the pipe.
			req, err := http.NewRequest(http.MethodGet, upstream.URL, nil)
			require.NoError(t, err)
			req.Header.Set("Connection", "close") // signal we won't reuse
			require.NoError(t, req.WriteProxy(edgeClient))

			// Read and discard the response.
			resp, err := http.ReadResponse(bufio.NewReader(edgeClient), req)
			require.NoError(t, err)
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close() //nolint:errcheck

			// Closing the edge connection signals httpServe to stop.
			edgeClient.Close() //nolint:errcheck
			<-done
		}()
	}
	wg.Wait()

	// Leak 1: all upstream connections must be released.
	assert.Eventually(t, func() bool {
		return activeUpstream.Load() == 0
	}, 5*time.Second, 50*time.Millisecond, "upstream connections were not released (Leak 1)")

	// Leak 2: goroutine count should return to baseline (allow a small
	// margin for Go runtime housekeeping goroutines).
	assert.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= baseGoroutines+2
	}, 5*time.Second, 50*time.Millisecond, "goroutines were not released (Leak 2)")
}
