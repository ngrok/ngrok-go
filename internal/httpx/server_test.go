package httpx

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"testing"
	"time"
)

// newTestServer returns a ServeConnServer backed by a trivial handler.
func newTestServer(t *testing.T) *ServeConnServer {
	t.Helper()

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "ok")
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	srv := NewServeConnServer(server, slog.New(slog.DiscardHandler))
	go srv.ListenAndServe()              //nolint:errcheck
	t.Cleanup(func() { server.Close() }) //nolint:errcheck

	return srv
}

// serveOneConn drives a single connection through ServeConn: one request, then
// the client hangs up. It blocks until ServeConn returns.
func serveOneConn(t *testing.T, srv *ServeConnServer, ctx context.Context) {
	t.Helper()

	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeConn(ctx, server, nil) //nolint:errcheck
	}()

	if _, err := io.WriteString(client, "GET / HTTP/1.1\r\nHost: example.test\r\n\r\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(client), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	resp.Body.Close() //nolint:errcheck
	client.Close()    //nolint:errcheck

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ServeConn did not return after the connection closed")
	}
}

// waitForGoroutines polls until the goroutine count drops to within limit of
// base, and reports the final delta.
func waitForGoroutines(base, limit int) int {
	deadline := time.Now().Add(3 * time.Second)
	delta := runtime.NumGoroutine() - base
	for time.Now().Before(deadline) && delta > limit {
		time.Sleep(25 * time.Millisecond)
		delta = runtime.NumGoroutine() - base
	}
	return delta
}

// A background context has a nil Done() channel. A watcher goroutine parked on
// it would never wake, leaking one goroutine per connection served, forever.
func TestServeConnNoGoroutineLeakBackgroundCtx(t *testing.T) {
	srv := newTestServer(t)

	const n = 50
	base := runtime.NumGoroutine()
	for range n {
		serveOneConn(t, srv, context.Background())
	}

	if delta := waitForGoroutines(base, 10); delta > 10 {
		t.Errorf("leaked goroutines: count grew by %d after %d connections", delta, n)
	}
}

// A long-lived cancelable context is the other half of the leak: a watcher
// parked on ctx.Done() survives until that context is canceled, so a forwarder
// that lives for days accumulates one parked goroutine per connection served.
func TestServeConnNoGoroutineLeakLongLivedCtx(t *testing.T) {
	srv := newTestServer(t)

	// Deliberately NOT canceled until the test ends, standing in for a context
	// that lives as long as the endpoint.
	ctx := t.Context()

	const n = 50
	base := runtime.NumGoroutine()
	for range n {
		serveOneConn(t, srv, ctx)
	}

	if delta := waitForGoroutines(base, 10); delta > 10 {
		t.Errorf("leaked goroutines: count grew by %d after %d connections on one long-lived context", delta, n)
	}
}

// Canceling the context must release a connection that is sitting idle between
// keep-alive requests.
func TestServeConnCtxCancelReleasesIdleConn(t *testing.T) {
	srv := newTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, server := net.Pipe()
	defer client.Close() //nolint:errcheck

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeConn(ctx, server, nil) //nolint:errcheck
	}()

	// One request, keep-alive, leaving the connection idle afterwards.
	if _, err := io.WriteString(client, "GET / HTTP/1.1\r\nHost: example.test\r\n\r\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(client), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close() //nolint:errcheck

	select {
	case <-done:
		t.Fatal("ServeConn returned while the connection was still usable")
	case <-time.After(100 * time.Millisecond):
	}

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ServeConn did not return after its context was canceled")
	}
}
