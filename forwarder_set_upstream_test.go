package ngrok

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/v2/internal/legacy"
	"golang.ngrok.com/ngrok/v2/internal/legacy/config"
)

func forward(t *testing.T, upstream *Upstream) *endpointForwarder {
	t.Helper()

	a := &agent{sess: &fakeUpdateSession{}, opts: defaultAgentOpts()}
	f, err := a.Forward(t.Context(), upstream)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	_, ok := f.(UpstreamSetter)
	require.True(t, ok, "Forward returns an UpstreamSetter")
	return f.(*endpointForwarder)
}

// bodyServer starts an HTTP server that answers every request with body, and
// returns its URL.
func bodyServer(t *testing.T, body string) string {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// fixedDialer dials addr, whatever address the forwarder asks for.
type fixedDialer struct{ addr string }

func (d fixedDialer) Dial(network, _ string) (net.Conn, error) {
	return net.Dial(network, d.addr)
}

func (d fixedDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	var nd net.Dialer
	return nd.DialContext(ctx, network, d.addr)
}

func dialerTo(t *testing.T, serverURL string) Dialer {
	t.Helper()

	u, err := url.Parse(serverURL)
	require.NoError(t, err)
	return fixedDialer{addr: u.Host}
}

type edgeConn struct {
	conn net.Conn
	r    *bufio.Reader
}

func openEdgeConn(t *testing.T, e *endpointForwarder) *edgeConn {
	conn := startEdgeConn(t, e)
	return &edgeConn{conn: conn, r: bufio.NewReader(conn)}
}

// get sends one request and returns the response body. It returns errors
// instead of failing the test, so any goroutine can call it.
func (c *edgeConn) get() (string, error) {
	if _, err := io.WriteString(c.conn, "GET / HTTP/1.1\r\nHost: example.test\r\n\r\n"); err != nil {
		return "", err
	}
	resp, err := http.ReadResponse(c.r, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}
	return string(body), nil
}

func get(t *testing.T, e *endpointForwarder) string {
	t.Helper()

	body, err := openEdgeConn(t, e).get()
	require.NoError(t, err)
	return body
}

// A file:// URL gives the SDK nothing to dial, so a "B" response proves that
// the forwarder used B's dialer.
func TestSetUpstreamSwitchesBetweenNetworkAndDialer(t *testing.T) {
	for name, endpoint := range map[string]func(*endpointForwarder){
		"https": httpsEndpoint,
		"tcp":   tcpEndpoint,
	} {
		t.Run(name, func(t *testing.T) {
			a := bodyServer(t, "A")
			e := forward(t, WithUpstream(a))
			endpoint(e)
			require.Equal(t, "A", get(t, e))

			bTLS := &tls.Config{ServerName: "b.example"}
			require.NoError(t, e.SetUpstream(WithUpstream("file:///srv/www",
				WithUpstreamDialer(dialerTo(t, bodyServer(t, "B"))),
				WithUpstreamTLSClientConfig(bTLS))))
			assert.Equal(t, "B", get(t, e))
			assert.Equal(t, url.URL{Scheme: "file", Path: "/srv/www"}, e.UpstreamURL())
			assert.Same(t, bTLS, e.UpstreamTLSClientConfig())

			require.NoError(t, e.SetUpstream(WithUpstream(a)))
			assert.Equal(t, "A", get(t, e))
			assert.Nil(t, e.UpstreamTLSClientConfig())
		})
	}
}

func TestSetUpstreamLeavesOpenConnectionOnOldUpstream(t *testing.T) {
	e := forward(t, WithUpstream(bodyServer(t, "A")))
	open := openEdgeConn(t, e)
	body, err := open.get()
	require.NoError(t, err)
	require.Equal(t, "A", body)

	require.NoError(t, e.SetUpstream(WithUpstream(bodyServer(t, "B"))))

	body, err = open.get()
	require.NoError(t, err)
	assert.Equal(t, "A", body, "the open connection still reaches A")
	assert.Equal(t, "B", get(t, e), "a new connection reaches B")
}

func TestSetUpstreamRejectsChangesFixedAtBind(t *testing.T) {
	e := forward(t, WithUpstream(bodyServer(t, "A"),
		WithUpstreamProtocol("http2"), WithUpstreamProxyProto(ProxyProtoV1)))
	b := bodyServer(t, "B")
	before := e.UpstreamURL()

	for name, u := range map[string]*Upstream{
		"nil":           nil,
		"protocol":      WithUpstream(b, WithUpstreamProxyProto(ProxyProtoV1)),
		"PROXY version": WithUpstream(b, WithUpstreamProtocol("http2"), WithUpstreamProxyProto(ProxyProtoV2)),
		"bad address":   WithUpstream("://no-scheme", WithUpstreamProtocol("http2"), WithUpstreamProxyProto(ProxyProtoV1)),
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, e.SetUpstream(u))
			assert.Equal(t, before, e.UpstreamURL())
		})
	}

	require.NoError(t, e.SetUpstream(
		WithUpstream(b, WithUpstreamProtocol("http2"), WithUpstreamProxyProto(ProxyProtoV1))))
	assert.Equal(t, "B", get(t, e))
}

func TestUpdateUpstreamKeepsTheDialer(t *testing.T) {
	e := forward(t, WithUpstream(bodyServer(t, "A")))
	require.NoError(t, e.SetUpstream(WithUpstream("file:///srv/www",
		WithUpstreamDialer(dialerTo(t, bodyServer(t, "B"))))))

	// The SDK cannot dial an .invalid host, so "B" proves the dialer stayed.
	next := url.URL{Scheme: "http", Host: "b.invalid"}
	e.UpdateUpstream(next)

	assert.Equal(t, next, e.UpstreamURL())
	assert.Equal(t, "B", get(t, e))
}

// A's http URL takes the HTTP proxy path, and B's file:// URL takes the raw
// path through B's dialer. A connection that mixed A's URL with B's, or used
// B's URL without B's dialer, gets an error instead of a body.
func TestSetUpstreamConcurrentConnectionsSeeOneUpstream(t *testing.T) {
	a := WithUpstream(bodyServer(t, "A"))
	b := WithUpstream("file:///srv/www", WithUpstreamDialer(dialerTo(t, bodyServer(t, "B"))))
	e := forward(t, a)

	stop := make(chan struct{})
	flipped := make(chan struct{})
	go func() {
		defer close(flipped)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			next := a
			if i%2 == 0 {
				next = b
			}
			assert.NoError(t, e.SetUpstream(next))
		}
	}()

	var wg sync.WaitGroup
	for range 200 {
		wg.Go(func() {
			body, err := openEdgeConn(t, e).get()
			if assert.NoError(t, err) {
				assert.Contains(t, []string{"A", "B"}, body)
			}
		})
	}
	wg.Wait()
	close(stop)
	<-flipped
}

type listenCountingSession struct {
	fakeUpdateSession
	listens int
}

func (s *listenCountingSession) Listen(ctx context.Context, cfg config.Tunnel) (legacy.Tunnel, error) {
	s.listens++
	return s.fakeUpdateSession.Listen(ctx, cfg)
}

func TestForwardRejectsBadAddressBeforeBinding(t *testing.T) {
	sess := &listenCountingSession{}
	a := &agent{sess: sess, opts: defaultAgentOpts()}

	_, err := a.Forward(t.Context(), WithUpstream("://no-scheme"))

	require.Error(t, err)
	assert.Zero(t, sess.listens, "Forward bound no tunnel")
}
