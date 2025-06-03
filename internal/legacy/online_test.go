package legacy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/websocket"

	"golang.ngrok.com/ngrok/v2/internal/legacy/config"
)

func newTestLogger(t *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})).WithGroup(t.Name())
}

func expectChanError(t *testing.T, ch <-chan error, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-ch:
		require.Error(t, err)
	case <-timer.C:
		t.Error("timeout while waiting on error channel")
	}
}

func skipUnless(t *testing.T, varname string, message ...any) {
	if os.Getenv(varname) == "" && os.Getenv("NGROK_TEST_ALL") == "" {
		t.Skip(message...)
	}
}

func onlineTest(t *testing.T) {
	skipUnless(t, "NGROK_TEST_ONLINE", "Skipping online test")
	// This is an annoying quirk of the free account limitations. It looks like
	// the tests run quickly enough in series that they trigger simultaneous
	// session errors for free accounts. "Something something eventual
	// consistency" most likely.
	require.NotEmpty(t, os.Getenv("NGROK_AUTHTOKEN"), "Online tests require an authtoken.")
}

func setupSession(ctx context.Context, t *testing.T, opts ...ConnectOption) Session {
	onlineTest(t)
	opts = append(opts, WithAuthtoken(os.Getenv("NGROK_AUTHTOKEN")), WithLogger(newTestLogger(t)))
	sess, err := Connect(ctx, opts...)
	require.NoError(t, err, "Session Connect")
	return sess
}

func startTunnel(ctx context.Context, t *testing.T, sess Session, opts config.Tunnel) Tunnel {
	onlineTest(t)
	tun, err := sess.Listen(ctx, opts)
	require.NoError(t, err, "Listen")
	return tun
}

var helloHandler = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
	_, _ = io.ReadAll(r.Body)
	_ = r.Body.Close()
	_, _ = fmt.Fprintln(rw, "Hello, world!")
})

func serveHTTP(ctx context.Context, t *testing.T, connectOpts []ConnectOption, opts config.Tunnel, handler http.Handler) (Tunnel, <-chan error) {
	sess := setupSession(ctx, t, connectOpts...)

	tun := startTunnel(ctx, t, sess, opts)
	exited := make(chan error)

	go func() {
		exited <- http.Serve(tun, handler)
		sess.Close()
	}()
	return tun, exited
}

func TestTunnel(t *testing.T) {
	ctx := context.Background()
	sess := setupSession(ctx, t)

	tun := startTunnel(ctx, t, sess, config.HTTPEndpoint(
		config.WithMetadata("Hello, world!"),
		config.WithForwardsTo("some application")))

	require.NotEmpty(t, tun.URL(), "Tunnel URL")
	require.Equal(t, "Hello, world!", tun.Metadata())
	require.Equal(t, "some application", tun.ForwardsTo())
	tun.Close()
	sess.Close()
}

func TestTunnelConnMetadata(t *testing.T) {
	ctx := context.Background()
	sess := setupSession(ctx, t)

	tun := startTunnel(ctx, t, sess, config.HTTPEndpoint())

	go func() {
		resp, _ := http.Get(tun.URL())
		if resp != nil {
			_ = resp.Body.Close()
		}
	}()

	conn, err := tun.Accept()
	require.NoError(t, err)

	proxyconn, ok := conn.(Conn)
	require.True(t, ok, "conn doesn't implement proxy conn interface")

	require.Equal(t, "https", proxyconn.Proto())
	tun.Close()
	sess.Close()
}

// *testing.T wrapper to force `require` to Fail() then panic() rather than
// FailNow(). Permits better flow control in test functions.
type failPanic struct {
	t *testing.T
}

func (f failPanic) Errorf(format string, args ...interface{}) {
	f.t.Errorf(format, args...)
}

func (f failPanic) FailNow() {
	f.t.Fail()
	panic("test failed")
}

func TestTCP(t *testing.T) {
	onlineTest(t)
	ctx := context.Background()

	opts := config.TCPEndpoint()

	// Easier to test by pretending it's HTTP on this end.
	tun, exited := serveHTTP(ctx, t, nil, opts, helloHandler)

	url, err := url.Parse(tun.URL())
	require.NoError(t, err)
	url.Scheme = "http"
	resp, err := http.Get(url.String())
	require.NoError(t, err, "GET tunnel url")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Read response body")

	require.Equal(t, "Hello, world!\n", string(body), "HTTP Body Contents")

	require.NoError(t, tun.CloseWithContext(ctx))
	expectChanError(t, exited, 5*time.Second)
}

func TestConnectionCallbacks(t *testing.T) {
	// Don't run this one by default - it's timing-sensitive and prone to flakes
	skipUnless(t, "NGROK_TEST_FLAKEY", "Skipping flakey network test")

	ctx := context.Background()
	connects := 0
	disconnectErrs := 0
	disconnectNils := 0
	sess := setupSession(ctx, t,
		WithConnectHandler(func(ctx context.Context, sess Session) {
			connects++
		}),
		WithDisconnectHandler(func(ctx context.Context, sess Session, err error) {
			if err == nil {
				disconnectNils++
			} else {
				disconnectErrs++
			}
		}),
		WithDialer(&sketchyDialer{1 * time.Second}))

	time.Sleep(2*time.Second + 500*time.Millisecond)

	_ = sess.Close()

	time.Sleep(2 * time.Second)

	require.Equal(t, 3, connects, "should've seen some connect events")
	require.Equal(t, 3, disconnectErrs, "should've seen some errors from disconnecting")
	require.Equal(t, 1, disconnectNils, "should've seen a final nil from disconnecting")
}

type sketchyDialer struct {
	limit time.Duration
}

func (sd *sketchyDialer) Dial(network, addr string) (net.Conn, error) {
	return sd.DialContext(context.Background(), network, addr)
}

func (sd *sketchyDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	conn, err := net.Dial(network, addr)
	go func() {
		time.Sleep(sd.limit)
		conn.Close()
	}()
	return conn, err
}

func TestHeartbeatCallback(t *testing.T) {
	// Don't run this one by default - it's long
	skipUnless(t, "NGROK_TEST_LONG", "Skipping long network test")

	ctx := context.Background()
	heartbeats := 0
	sess := setupSession(ctx, t,
		WithHeartbeatHandler(func(ctx context.Context, sess Session, latency time.Duration) {
			heartbeats++
		}),
		WithHeartbeatInterval(10*time.Second))

	time.Sleep(20*time.Second + 500*time.Millisecond)

	_ = sess.Close()

	require.Equal(t, 2, heartbeats, "should've seen some heartbeats")
}

func TestPermanentErrors(t *testing.T) {
	onlineTest(t)
	var err error
	ctx := context.Background()
	token := os.Getenv("NGROK_AUTHTOKEN")

	sess, err := Connect(ctx, WithAuthtoken(token))
	require.NoError(t, err)
	sess.Close()

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_, err = Connect(timeoutCtx, WithServer("127.0.0.234:123"), WithAuthtoken(token))
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRetryableErrors(t *testing.T) {
	onlineTest(t)
	var err error
	// Set global context with a longer timeout just to prevent test from hanging forever
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	// Create a custom dialer with short timeout for invalid addresses
	dialer := &net.Dialer{Timeout: 500 * time.Millisecond}

	// give up on connecting after first attempt
	disconnect := WithDisconnectHandler(func(_ context.Context, sess Session, disconnectErr error) {
		sess.Close()
	})
	connect := WithConnectHandler(func(_ context.Context, sess Session) {
		sess.Close()
	})

	_, err = Connect(ctx, WithServer("127.0.0.234:123"), WithDialer(dialer), connect, disconnect)
	var dialErr errSessionDial
	require.ErrorIs(t, err, dialErr)
	require.ErrorAs(t, err, &dialErr)

	_, err = Connect(ctx, WithAuthtoken("invalid-token"), WithDialer(dialer), connect, disconnect)
	var authErr errAuthFailed
	require.ErrorIs(t, err, authErr)
	require.ErrorAs(t, err, &authErr)
	require.True(t, authErr.Remote)
}

func TestNonExported(t *testing.T) {
	ctx := context.Background()

	sess := setupSession(ctx, t)

	require.NotEmpty(t, sess.(interface{ Region() string }).Region())
}

func echo(ws *websocket.Conn) {
	_, _ = io.Copy(ws, ws)
}

func TestWebsockets(t *testing.T) {
	onlineTest(t)

	ctx := context.Background()

	srv := &http.ServeMux{}
	srv.Handle("/", helloHandler)
	srv.Handle("/ws", websocket.Handler(echo))

	tun, errCh := serveHTTP(ctx, t, nil, config.HTTPEndpoint(config.WithScheme(config.SchemeHTTPS)), srv)

	tunnelURL, err := url.Parse(tun.URL())
	require.NoError(t, err)

	conn, err := websocket.Dial(fmt.Sprintf("wss://%s/ws", tunnelURL.Hostname()), "", tunnelURL.String())
	require.NoError(t, err)

	go func() {
		_, _ = fmt.Fprintln(conn, "Hello, world!")
	}()

	bufConn := bufio.NewReader(conn)
	out, err := bufConn.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "Hello, world!\n", out)

	conn.Close()
	tun.Close()

	require.Error(t, <-errCh)
}
