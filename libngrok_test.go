package libngrok

import (
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/websocket"
)

func setupSession(ctx context.Context, t *testing.T, opts *ConnectConfig) Session {
	if opts == nil {
		opts = ConnectOptions()
	}
	opts.WithAuthToken(os.Getenv("NGROK_TOKEN"))
	sess, err := Connect(ctx, opts)
	require.NoError(t, err, "Session Connect")
	return sess
}

func startTunnel(ctx context.Context, t *testing.T, sess Session, opts TunnelConfig) Tunnel {
	tun, err := sess.StartTunnel(ctx, opts)
	require.NoError(t, err, "StartTunnel")
	return tun
}

var helloHandler = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
	_, _ = fmt.Fprintln(rw, "Hello, world!")
})

func serveHTTP(ctx context.Context, t *testing.T, connectOpts *ConnectConfig, opts TunnelConfig, handler http.Handler) (Tunnel, <-chan error) {
	sess := setupSession(ctx, t, connectOpts)

	tun := startTunnel(ctx, t, sess, opts)
	exited := make(chan error)

	httpTun := tun.AsHTTP()

	go func() {
		exited <- httpTun.Serve(ctx, handler)
	}()
	return tun, exited
}

func TestTunnel(t *testing.T) {
	ctx := context.Background()
	sess := setupSession(ctx, t, nil)

	tun := startTunnel(ctx, t, sess, HTTPOptions().
		WithMetadata("Hello, world!").
		WithForwardsTo("some application"))

	require.NotEmpty(t, tun.URL(), "Tunnel URL")
	require.Equal(t, "Hello, world!", tun.Metadata())
	require.Equal(t, "some application", tun.ForwardsTo())
}

func TestHTTPS(t *testing.T) {
	ctx := context.Background()
	tun, exited := serveHTTP(ctx, t, nil,
		HTTPOptions(),
		helloHandler,
	)

	resp, err := http.Get(tun.URL())
	require.NoError(t, err, "GET tunnel url")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Read response body")

	require.Equal(t, "Hello, world!\n", string(body), "HTTP Body Contents")

	require.NotNil(t, resp.TLS, "TLS established")

	// Closing the tunnel should be fine
	require.NoError(t, tun.CloseWithContext(ctx))

	// The http server should exit with a "closed" error
	require.Error(t, <-exited)
}

func TestHTTP(t *testing.T) {
	ctx := context.Background()
	tun, exited := serveHTTP(ctx, t, nil,
		HTTPOptions().
			WithScheme(SchemeHTTP),
		helloHandler,
	)

	resp, err := http.Get(tun.URL())
	require.NoError(t, err, "GET tunnel url")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Read response body")

	require.Equal(t, "Hello, world!\n", string(body), "HTTP Body Contents")

	require.Nil(t, resp.TLS, "No TLS")

	// Closing the tunnel should be fine
	require.NoError(t, tun.CloseWithContext(ctx))

	// The http server should exit with a "closed" error
	require.Error(t, <-exited)
}

func TestHTTPCompression(t *testing.T) {
	ctx := context.Background()
	opts := HTTPOptions().WithCompression()
	tun, exited := serveHTTP(ctx, t, nil, opts, helloHandler)

	req, err := http.NewRequest(http.MethodGet, tun.URL(), nil)
	require.NoError(t, err, "Create request")
	req.Header.Add("Accept-Encoding", "gzip")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "GET tunnel url")

	require.Equal(t, http.StatusOK, resp.StatusCode)

	gzReader, err := gzip.NewReader(resp.Body)
	require.NoError(t, err, "gzip reader")

	body, err := io.ReadAll(gzReader)
	require.NoError(t, err, "Read response body")

	require.Equal(t, "Hello, world!\n", string(body), "HTTP Body Contents")

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
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

func TestHTTPHeaders(t *testing.T) {
	ctx := context.Background()
	opts := HTTPOptions().
		WithRequestHeaders(HTTPHeaders().
			Add("foo", "bar").
			Remove("baz")).
		WithResponseHeaders(HTTPHeaders().
			Add("spam", "eggs").
			Remove("python"))

	tun, exited := serveHTTP(ctx, t, nil, opts, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		defer func() { _ = recover() }()
		t := failPanic{t}

		require.NotContains(t, r.Header, "Baz", "Baz Removed")
		require.Contains(t, r.Header, "Foo", "Foo added")
		require.Equal(t, "bar", r.Header.Get("Foo"), "Foo=bar")

		rw.Header().Add("Python", "bad header")
		_, _ = fmt.Fprintln(rw, "Hello, world!")
	}))

	req, err := http.NewRequest(http.MethodGet, tun.URL(), nil)
	require.NoError(t, err, "Create request")
	req.Header.Add("Baz", "bad header")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "GET tunnel url")

	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Read response body")

	require.Equal(t, "Hello, world!\n", string(body), "HTTP Body Contents")

	require.NotContains(t, resp.Header, "Python", "Python removed")
	require.Contains(t, resp.Header, "Spam", "Spam added")
	require.Equal(t, "eggs", resp.Header.Get("Spam"), "Spam=eggs")

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

func TestBasicAuth(t *testing.T) {
	ctx := context.Background()

	opts := HTTPOptions().WithBasicAuth("user", "foobarbaz")

	tun, exited := serveHTTP(ctx, t, nil, opts, helloHandler)

	req, err := http.NewRequest(http.MethodGet, tun.URL(), nil)
	require.NoError(t, err, "Create request")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "GET tunnel url")

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	req.SetBasicAuth("user", "foobarbaz")

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err, "GET tunnel url")

	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Read response body")

	require.Equal(t, "Hello, world!\n", string(body), "HTTP Body Contents")

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

func TestCircuitBreaker(t *testing.T) {
	// Don't run this one by default - it has to make ~50 requests.
	if os.Getenv("NGROK_TEST_LONG") == "" {
		t.Skip("Skipping long circuit breaker test")
		return
	}
	ctx := context.Background()

	opts := HTTPOptions().WithCircuitBreaker(0.1)

	n := 0
	tun, exited := serveHTTP(ctx, t, nil, opts, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n = n + 1
		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	var (
		resp *http.Response
		err  error
	)

	for i := 0; i < 50; i++ {
		resp, err = http.Get(tun.URL())
		require.NoError(t, err)
	}

	// Should see fewer than 50 requests come through.
	require.Less(t, n, 50)

	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

type TestConfig interface {
	TunnelConfig
	WithProxyProtoI(version ProxyProtoVersion) TunnelConfig
}

func (http *HTTPConfig) WithProxyProtoI(version ProxyProtoVersion) TunnelConfig {
	return http.WithProxyProto(version)
}

func (tcp *TCPConfig) WithProxyProtoI(version ProxyProtoVersion) TunnelConfig {
	return tcp.WithProxyProto(version)
}

func TestProxyProto(t *testing.T) {
	ctx := context.Background()

	type testCase struct {
		name          string
		optsFunc      func() TestConfig
		reqFunc       func(*testing.T, string)
		version       ProxyProtoVersion
		shouldContain string
	}

	base := []testCase{
		{
			version:       ProxyProtoV1,
			shouldContain: "PROXY TCP4",
		},
		{
			version:       ProxyProtoV2,
			shouldContain: "\x0D\x0A\x0D\x0A\x00\x0D\x0A\x51\x55\x49\x54\x0A",
		},
	}

	var cases []testCase

	for _, c := range base {
		cases = append(cases,
			testCase{
				name:     fmt.Sprintf("HTTP/Version%d", c.version),
				optsFunc: func() TestConfig { return HTTPOptions() },
				reqFunc: func(t *testing.T, url string) {
					_, _ = http.Get(url)
				},
				version:       c.version,
				shouldContain: c.shouldContain,
			},
			testCase{
				name:     fmt.Sprintf("TCP/Version%d", c.version),
				optsFunc: func() TestConfig { return TCPOptions() },
				reqFunc: func(t *testing.T, u string) {
					url, err := url.Parse(u)
					require.NoError(t, err)
					conn, err := net.Dial("tcp", fmt.Sprintf("%s:%s", url.Hostname(), url.Port()))
					require.NoError(t, err)
					_, _ = fmt.Fprint(conn, "Hello, world!")
				},
				version:       c.version,
				shouldContain: c.shouldContain,
			},
		)
	}

	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			sess := setupSession(ctx, t, nil)
			tun := startTunnel(ctx, t, sess, tcase.optsFunc().
				WithProxyProtoI(tcase.version),
			).AsListener()

			go tcase.reqFunc(t, tun.URL())

			conn, err := tun.Accept()
			require.NoError(t, err, "Accept connection")

			buf := make([]byte, 12)
			_, err = io.ReadAtLeast(conn, buf, 12)
			require.NoError(t, err, "Read connection contents")

			conn.Close()

			require.Contains(t, string(buf), tcase.shouldContain)
		})
	}
}

func TestHostname(t *testing.T) {
	ctx := context.Background()

	tun, exited := serveHTTP(ctx, t, nil,
		HTTPOptions().WithDomain("foo.robsonchase.com"),
		helloHandler,
	)
	require.Equal(t, "https://foo.robsonchase.com", tun.URL())

	resp, err := http.Get(tun.URL())
	require.NoError(t, err)

	content, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "Hello, world!\n", string(content))

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

func TestSubdomain(t *testing.T) {
	ctx := context.Background()

	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, rand.Uint64())

	subdomain := hex.EncodeToString(buf)

	tun, exited := serveHTTP(ctx, t, nil,
		HTTPOptions().WithDomain(subdomain+".ngrok.io"),
		helloHandler,
	)

	require.Contains(t, tun.URL(), subdomain)

	resp, err := http.Get(tun.URL())
	require.NoError(t, err)

	content, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "Hello, world!\n", string(content))

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

func TestOAuth(t *testing.T) {
	ctx := context.Background()

	opts := HTTPOptions().WithOAuth(OAuthProvider("google"))

	tun, exited := serveHTTP(ctx, t, nil, opts, helloHandler)

	resp, err := http.Get(tun.URL())
	require.NoError(t, err, "GET tunnel url")

	content, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NotContains(t, string(content), "Hello, world!")

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

func TestHTTPIPRestriction(t *testing.T) {
	ctx := context.Background()

	_, cidr, err := net.ParseCIDR("0.0.0.0/0")
	require.NoError(t, err)

	opts := HTTPOptions().WithCIDRRestriction(
		CIDRSet().
			AllowString("127.0.0.1/32").
			Deny(cidr),
	)

	tun, exited := serveHTTP(ctx, t, nil, opts, helloHandler)

	resp, err := http.Get(tun.URL())
	require.NoError(t, err, "GET tunnel url")

	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

func TestTCP(t *testing.T) {
	ctx := context.Background()

	opts := TCPOptions()

	// Easier to test by pretending it's HTTP on this end.
	tun, exited := serveHTTP(ctx, t, nil, opts, helloHandler)

	url, err := url.Parse(tun.URL())
	require.NoError(t, err)
	url.Scheme = "http"
	resp, err := http.Get(url.String())
	require.NoError(t, err, "GET tunnel url")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Read response body")

	require.Equal(t, "Hello, world!\n", string(body), "HTTP Body Contents")

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

func TestTCPIPRestriction(t *testing.T) {
	ctx := context.Background()

	_, cidr, err := net.ParseCIDR("127.0.0.1/32")
	require.NoError(t, err)

	opts := TCPOptions().WithCIDRRestriction(
		CIDRSet().
			Allow(cidr).
			DenyString("0.0.0.0/0"),
	)

	// Easier to test by pretending it's HTTP on this end.
	tun, exited := serveHTTP(ctx, t, nil, opts, helloHandler)

	url, err := url.Parse(tun.URL())
	require.NoError(t, err)
	url.Scheme = "http"
	_, err = http.Get(url.String())

	// Rather than layer-7 error, we should see it at the connection level
	require.Error(t, err, "GET Tunnel URL")

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

func TestLabeled(t *testing.T) {
	ctx := context.Background()
	tun, exited := serveHTTP(ctx, t, nil,
		LabeledOptions().
			WithLabel("edge", "edghts_2CtuOWQFCrvggKT34fRCFXs0AiK").
			WithMetadata("Hello, world!"),
		helloHandler,
	)

	require.Equal(t, "Hello, world!", tun.Metadata())

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)

	for {
		require.NoError(t, ctx.Err(), "context deadline reached while waiting for edge")
		resp, err := http.Get("https://kzu7214a.ngrok.io/")
		require.NoError(t, err, "GET tunnel url")

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err, "Read response body")

		if string(body) == "Hello, world!\n" {
			break
		}
	}

	cancel()

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

func TestWebsocketConversion(t *testing.T) {
	ctx := context.Background()
	sess := setupSession(ctx, t, nil)
	tun := startTunnel(ctx, t, sess,
		HTTPOptions().
			WithWebsocketTCPConversion(),
	)

	// HTTP over websockets? suuuure lol
	exited := make(chan error)
	go func() {
		exited <- http.Serve(tun.AsListener(), helloHandler)
	}()

	resp, err := http.Get(tun.URL())
	require.NoError(t, err)

	require.Equal(t, http.StatusBadRequest, resp.StatusCode, "Normal http should be rejected")

	url, err := url.Parse(tun.URL())
	require.NoError(t, err)

	url.Scheme = "wss"

	client := http.Client{
		Transport: &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				return websocket.Dial(url.String(), "", tun.URL())
			},
		},
	}

	resp, err = client.Get("http://example.com")
	require.NoError(t, err)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Read response body")

	require.Equal(t, "Hello, world!\n", string(body), "HTTP Body Contents")

	require.NoError(t, tun.CloseWithContext(ctx))
	require.Error(t, <-exited)
}

func TestConnectcionCallbacks(t *testing.T) {
	// Don't run this one by default - it's timing-sensitive and prone to flakes
	if os.Getenv("NGROK_TEST_FLAKEY") == "" {
		t.Skip("Skipping flakey network test")
		return
	}

	ctx := context.Background()
	connects := 0
	disconnectErrs := 0
	disconnectNils := 0
	sess := setupSession(ctx, t, ConnectOptions().WithLocalCallbacks(LocalCallbacks{
		OnConnect: func(ctx context.Context, sess Session) {
			connects += 1
		},
		OnDisconnect: func(ctx context.Context, sess Session, err error) {
			if err == nil {
				disconnectNils += 1
			} else {
				disconnectErrs += 1
			}
		},
	}).WithDialer(&sketchyDialer{1 * time.Second}))

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
	if os.Getenv("NGROK_TEST_LONG") == "" {
		t.Skip("Skipping long network test")
		return
	}

	ctx := context.Background()
	heartbeats := 0
	sess := setupSession(ctx, t, ConnectOptions().WithLocalCallbacks(LocalCallbacks{
		OnHeartbeat: func(ctx context.Context, sess Session, latency time.Duration) {
			heartbeats += 1
		},
	}).WithHeartbeatInterval(10*time.Second))

	time.Sleep(20*time.Second + 500*time.Millisecond)

	_ = sess.Close()

	require.Equal(t, 2, heartbeats, "should've seen some heartbeats")
}

func TestErrors(t *testing.T) {
	var err error
	ctx := context.Background()
	u, _ := url.Parse("notarealscheme://example.com")

	_, err = Connect(ctx, ConnectOptions().WithProxyURL(u))
	var proxyErr ErrProxyInit
	require.ErrorIs(t, err, proxyErr)
	require.ErrorAs(t, err, &proxyErr)

	_, err = Connect(ctx, ConnectOptions().WithServer("127.0.0.234:123"))
	var dialErr ErrSessionDial
	require.ErrorIs(t, err, dialErr)
	require.ErrorAs(t, err, &dialErr)

	_, err = Connect(ctx, ConnectOptions().WithAuthToken("lolnope"))
	var authErr ErrAuthFailed
	require.ErrorIs(t, err, authErr)
	require.ErrorAs(t, err, &authErr)
	require.True(t, authErr.Context.Remote)

	sess, err := Connect(ctx, ConnectOptions())
	require.NoError(t, err)
	_, err = sess.StartTunnel(ctx, TCPOptions())
	var startErr ErrStartTunnel
	require.ErrorIs(t, err, startErr)
	require.ErrorAs(t, err, &startErr)
	require.IsType(t, &TCPConfig{}, startErr.Context.Config)
}
