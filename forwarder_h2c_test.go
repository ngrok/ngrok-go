package ngrok

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/v2/internal/legacy"
	"golang.ngrok.com/ngrok/v2/internal/legacy/config"
)

// newH2CUpstreamHandler starts a server that, like grpc-go, accepts only HTTP/2
// without TLS (h2c), serving requests with h.
func newH2CUpstreamHandler(t *testing.T, h http.Handler) string {
	t.Helper()

	srv := httptest.NewUnstartedServer(h)
	srv.Config.Protocols = new(http.Protocols)
	srv.Config.Protocols.SetUnencryptedHTTP2(true)
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}

// newH2CUpstream starts a server that, like grpc-go, accepts only HTTP/2
// without TLS (h2c). It answers with the protocol the request arrived over.
func newH2CUpstream(t *testing.T) string {
	t.Helper()

	return newH2CUpstreamHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Proto)
	}))
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

// singleConnDialer returns a DialContext function that yields conn once and
// fails any subsequent dial attempt instead of handing back the same conn.
func singleConnDialer(conn net.Conn) func(context.Context, string, string) (net.Conn, error) {
	var dialed atomic.Bool
	return func(context.Context, string, string) (net.Conn, error) {
		if !dialed.CompareAndSwap(false, true) {
			return nil, errors.New("test edge connection already dialed")
		}
		return conn, nil
	}
}

type listenCapturingSession struct {
	fakeUpdateSession
	cfgs []config.Tunnel
}

func (s *listenCapturingSession) Listen(ctx context.Context, cfg config.Tunnel) (legacy.Tunnel, error) {
	s.cfgs = append(s.cfgs, cfg)
	return s.fakeUpdateSession.Listen(ctx, cfg)
}

func forwardsProto(t *testing.T, cfg config.Tunnel) string {
	t.Helper()

	p, ok := cfg.(interface{ ForwardsProto() string })
	require.True(t, ok, "tunnel config %T does not expose ForwardsProto", cfg)
	return p.ForwardsProto()
}

// Forward must send the app protocol hint to the edge on HTTP/HTTPS endpoints
// so the edge knows to speak HTTP/2 down the tunnel.
func TestForwardSendsAppProtocolHint(t *testing.T) {
	tests := []struct {
		name     string
		proto    string
		opts     []EndpointOption
		wantHint string
	}{
		{
			name:     "http2 on default https endpoint",
			proto:    "http2",
			wantHint: "http2",
		},
		{
			name:     "http2 on explicit http endpoint",
			proto:    "http2",
			opts:     []EndpointOption{WithURL("http://example.ngrok.app")},
			wantHint: "http2",
		},
		{
			name:     "http1",
			proto:    "http1",
			wantHint: "http1",
		},
		{
			name:     "uppercase HTTP1 normalized",
			proto:    "HTTP1",
			wantHint: "http1",
		},
		{
			name:     "unset protocol sends empty hint",
			proto:    "",
			wantHint: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sess := &listenCapturingSession{}
			a := &agent{sess: sess, opts: defaultAgentOpts()}

			var upstreamOpts []UpstreamOption
			if tc.proto != "" {
				upstreamOpts = append(upstreamOpts, WithUpstreamProtocol(tc.proto))
			}
			f, err := a.Forward(t.Context(), WithUpstream("http://127.0.0.1:8080", upstreamOpts...), tc.opts...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = f.Close() })

			require.Len(t, sess.cfgs, 1)
			assert.Equal(t, tc.wantHint, forwardsProto(t, sess.cfgs[0]))
			assert.Equal(t, tc.wantHint, f.UpstreamProtocol())
		})
	}
}

// The hint sent to the edge, the tunnel-side server, the upstream transport,
// and SetUpstream must all agree on protocol casing so mixed-case values like
// "HTTP2" do not cause edge/agent mismatches.
func TestForwardNormalizesUpstreamProtocol(t *testing.T) {
	sess := &listenCapturingSession{}
	a := &agent{sess: sess, opts: defaultAgentOpts()}

	upstream1 := newH2CUpstream(t)
	f, err := a.Forward(t.Context(), WithUpstream(upstream1, WithUpstreamProtocol("HTTP2")))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	require.Len(t, sess.cfgs, 1)
	assert.Equal(t, "http2", forwardsProto(t, sess.cfgs[0]))
	assert.Equal(t, "http2", f.UpstreamProtocol())

	// An h2c request from the edge must be accepted by the tunnel-side server
	// and forwarded over h2c to the upstream.
	ef := f.(*endpointForwarder)
	edgeClient := startEdgeConn(t, ef)
	edge := &http.Transport{
		Protocols:   new(http.Protocols),
		DialContext: singleConnDialer(edgeClient),
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

	// SetUpstream must accept both the original casing and the normalized form.
	upstream2 := newH2CUpstream(t)
	setter, ok := f.(UpstreamSetter)
	require.True(t, ok)
	assert.NoError(t, setter.SetUpstream(WithUpstream(upstream2, WithUpstreamProtocol("HTTP2"))))
	assert.NoError(t, setter.SetUpstream(WithUpstream(upstream2, WithUpstreamProtocol("http2"))))
}

// Forward must reject unrecognized upstream protocols before binding a tunnel,
// so the edge is never sent a hint the agent cannot honor.
func TestForwardRejectsUnsupportedUpstreamProtocol(t *testing.T) {
	for _, proto := range []string{"h2c", "http/2", "bogus"} {
		t.Run(proto, func(t *testing.T) {
			sess := &listenCapturingSession{}
			a := &agent{sess: sess, opts: defaultAgentOpts()}

			_, err := a.Forward(t.Context(), WithUpstream("http://127.0.0.1:8080", WithUpstreamProtocol(proto)))

			require.Error(t, err)
			assert.Empty(t, sess.cfgs, "Forward bound no tunnel")
		})
	}
}

// gRPC carries status codes in HTTP/2 trailers. Both the tunnel-side server and
// the h2c upstream transport must preserve response trailers end to end.
func TestForwarderHTTP2PreservesTrailers(t *testing.T) {
	upstreamURL := newH2CUpstreamHandler(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Trailer", "Grpc-Status, Grpc-Message")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "payload")
		w.Header().Set("Grpc-Status", "0")
		w.Header().Set("Grpc-Message", "ok")
	}))

	e := newTestForwarder(t, upstreamURL)
	e.upstreamProtocol = "http2"
	httpsEndpoint(e)

	edgeClient := startEdgeConn(t, e)
	edge := &http.Transport{
		Protocols:       new(http.Protocols),
		DialContext:     singleConnDialer(edgeClient),
		MaxConnsPerHost: 1,
	}
	edge.Protocols.SetUnencryptedHTTP2(true)
	t.Cleanup(edge.CloseIdleConnections)

	resp, err := (&http.Client{Transport: edge}).Get("http://example.test/")
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "payload", string(body))
	assert.Equal(t, "0", resp.Trailer.Get("Grpc-Status"))
	assert.Equal(t, "ok", resp.Trailer.Get("Grpc-Message"))
}

// Bidirectional gRPC streaming relies on the upstream writing and flushing
// response bytes while the request body is still open.
func TestForwarderHTTP2FullDuplexStreaming(t *testing.T) {
	upstreamURL := newH2CUpstreamHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		br := bufio.NewReader(r.Body)
		rc := http.NewResponseController(w)

		line1, err := br.ReadString('\n')
		if err != nil {
			return
		}
		_, _ = io.WriteString(w, "echo:"+line1)
		_ = rc.Flush()

		line2, err := br.ReadString('\n')
		if err != nil {
			return
		}
		_, _ = io.WriteString(w, "echo:"+line2)
	}))

	e := newTestForwarder(t, upstreamURL)
	e.upstreamProtocol = "http2"
	httpsEndpoint(e)

	edgeClient := startEdgeConn(t, e)
	edge := &http.Transport{
		Protocols:       new(http.Protocols),
		DialContext:     singleConnDialer(edgeClient),
		MaxConnsPerHost: 1,
	}
	edge.Protocols.SetUnencryptedHTTP2(true)
	t.Cleanup(edge.CloseIdleConnections)

	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pw.Close()
		_ = pr.Close()
	})

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://example.test/stream", pr)
	require.NoError(t, err)

	writeFirstErr := make(chan error, 1)
	go func() {
		_, err := io.WriteString(pw, "first\n")
		writeFirstErr <- err
	}()

	resp, err := (&http.Client{Transport: edge}).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck
	require.NoError(t, <-writeFirstErr)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	br := bufio.NewReader(resp.Body)
	got1, err := br.ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "echo:first\n", got1)

	_, err = io.WriteString(pw, "second\n")
	require.NoError(t, err)
	require.NoError(t, pw.Close())

	got2, err := br.ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "echo:second\n", got2)

	rest, err := io.ReadAll(br)
	require.NoError(t, err)
	assert.Empty(t, rest)
}

// Concurrent HTTP/2 streams multiplexed over a single edge h2c connection must
// be served simultaneously and emit one EventHTTPRequestComplete per stream
// alongside one connection open/close pair.
func TestForwarderHTTP2ConcurrentStreamsEmitPerRequestMetrics(t *testing.T) {
	const n = 4
	var arrived atomic.Int32
	release := make(chan struct{})

	upstreamURL := newH2CUpstreamHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if arrived.Add(1) == n {
			close(release)
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, r.Proto)
	}))

	e, rec := newRecordedForwarder(t, upstreamURL)
	e.upstreamProtocol = "http2"
	httpsEndpoint(e)

	edgeClient := startEdgeConn(t, e)
	edge := &http.Transport{
		Protocols:       new(http.Protocols),
		DialContext:     singleConnDialer(edgeClient),
		MaxConnsPerHost: 1,
	}
	edge.Protocols.SetUnencryptedHTTP2(true)
	t.Cleanup(edge.CloseIdleConnections)

	client := &http.Client{Transport: edge}
	type result struct {
		status int
		body   string
		err    error
	}
	results := make([]result, n)

	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func() {
			defer wg.Done()
			resp, err := client.Get("http://example.test/")
			if err != nil {
				results[i].err = err
				return
			}
			defer resp.Body.Close() //nolint:errcheck
			body, err := io.ReadAll(resp.Body)
			results[i] = result{status: resp.StatusCode, body: string(body), err: err}
		}()
	}
	wg.Wait()

	for i, res := range results {
		require.NoError(t, res.err, "stream %d", i)
		assert.Equal(t, http.StatusOK, res.status, "stream %d", i)
		assert.Equal(t, "HTTP/2.0", res.body, "stream %d", i)
	}

	edge.CloseIdleConnections()
	require.NoError(t, edgeClient.Close())
	rec.waitFor(t, EventTypeConnectionClosed)

	reqs := eventsOf[*EventHTTPRequestComplete](rec)
	require.Len(t, reqs, n)
	for _, req := range reqs {
		assert.Equal(t, http.StatusOK, req.StatusCode)
	}
	assert.Len(t, eventsOf[*EventConnectionOpened](rec), 1)
	assert.Len(t, eventsOf[*EventConnectionClosed](rec), 1)
}

func TestSetUpstreamRejectsUnsupportedUpstreamProtocol(t *testing.T) {
	e := forward(t, WithUpstream(newH2CUpstream(t), WithUpstreamProtocol("http2")))
	before := e.UpstreamURL()

	err := e.SetUpstream(WithUpstream(newH2CUpstream(t), WithUpstreamProtocol("bogus")))

	require.Error(t, err)
	assert.Equal(t, before, e.UpstreamURL())
}
