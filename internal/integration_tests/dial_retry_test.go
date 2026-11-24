package integration_tests

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reserveClosedAddr returns a loopback address with nothing listening on it.
func reserveClosedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}

func tcpDial(address string) func(context.Context) (net.Conn, error) {
	return func(ctx context.Context) (net.Conn, error) {
		d := &net.Dialer{Timeout: 500 * time.Millisecond}
		return d.DialContext(ctx, "tcp", address)
	}
}

func TestDialWithRetryGivesUpAfterWindow(t *testing.T) {
	t.Parallel()
	addr := reserveClosedAddr(t)

	start := time.Now()
	conn, err := dialWithRetry(t.Context(), t, addr, time.Second, tcpDial(addr))
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Nil(t, conn)
	assert.Contains(t, err.Error(), "gave up after")
	assert.Less(t, elapsed, 3*time.Second, "dial retry should stop shortly after the window")
}

func TestDialWithRetryHonorsContext(t *testing.T) {
	t.Parallel()
	addr := reserveClosedAddr(t)

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := dialWithRetry(ctx, t, addr, time.Minute, tcpDial(addr))
	require.Error(t, err)
	assert.Less(t, time.Since(start), 2*time.Second, "dial retry should stop when ctx is done")
}

func TestDialWithRetrySucceedsOnceListening(t *testing.T) {
	t.Parallel()
	addr := reserveClosedAddr(t)

	// Start listening only after the first attempt has been refused.
	listening := make(chan net.Listener, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		l, err := net.Listen("tcp", addr)
		if err != nil {
			close(listening)
			return
		}
		listening <- l
	}()

	conn, err := dialWithRetry(t.Context(), t, addr, 5*time.Second, tcpDial(addr))
	l, ok := <-listening
	if !ok {
		t.Skip("could not re-listen on reserved port")
	}
	defer func() { _ = l.Close() }()
	require.NoError(t, err)
	require.NoError(t, conn.Close())
}
