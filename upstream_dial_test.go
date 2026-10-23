package ngrok

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// eventRecorder collects the events a forwarder emits, in order.
type eventRecorder struct {
	mu     sync.Mutex
	events []Event
}

// newRecordedForwarder is newTestForwarder with an agent that records events.
func newRecordedForwarder(t *testing.T, upstream string) (*endpointForwarder, *eventRecorder) {
	t.Helper()

	rec := &eventRecorder{}
	e := newTestForwarder(t, upstream)
	e.agent = &agent{eventHandlers: []EventHandler{func(evt Event) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.events = append(rec.events, evt)
	}}}

	return e, rec
}

func (r *eventRecorder) snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.events...)
}

func (r *eventRecorder) types() []EventType {
	var types []EventType
	for _, evt := range r.snapshot() {
		types = append(types, evt.EventType())
	}
	return types
}

func (r *eventRecorder) waitFor(t *testing.T, typ EventType) {
	t.Helper()

	require.Eventually(t, func() bool {
		for _, got := range r.types() {
			if got == typ {
				return true
			}
		}
		return false
	}, 5*time.Second, 10*time.Millisecond, "no %s event", typ)
}

func eventsOf[T Event](r *eventRecorder) []T {
	var events []T
	for _, evt := range r.snapshot() {
		if e, ok := evt.(T); ok {
			events = append(events, e)
		}
	}
	return events
}

// trusting makes the forwarder trust the test server's certificate.
func trusting(e *endpointForwarder, upstream *httptest.Server) {
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	e.upstreamTLSClientConfig = &tls.Config{RootCAs: roots}
}

// refusedAddr returns a loopback address with nothing listening on it.
func refusedAddr(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	return addr
}

// silentUpstream accepts TCP connections and never writes, like an upstream
// that stalls the TLS handshake.
func silentUpstream(t *testing.T) (string, <-chan net.Conn) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	accepted := make(chan net.Conn, 1)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = conn.Close() })
			accepted <- conn
		}
	}()

	return ln.Addr().String(), accepted
}

func tcpEndpoint(e *endpointForwarder) {
	e.endpointURL = url.URL{Scheme: "tcp", Host: "1.tcp.ngrok.example:12345"}
}

func httpsEndpoint(e *endpointForwarder) {
	e.endpointURL = url.URL{Scheme: "https", Host: "example.test"}
}

// sendRequest writes one GET to the edge connection, drains the response, and
// returns its status code.
func sendRequest(t *testing.T, edgeClient net.Conn, br *bufio.Reader) int {
	t.Helper()

	_, err := io.WriteString(edgeClient, "GET / HTTP/1.1\r\nHost: example.test\r\n\r\n")
	require.NoError(t, err)
	resp, err := http.ReadResponse(br, nil)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	return resp.StatusCode
}

func TestForwarderTCPEndpointRefusedUpstreamEmitsDialError(t *testing.T) {
	e, rec := newRecordedForwarder(t, "tcp://"+refusedAddr(t))
	tcpEndpoint(e)

	edgeClient := startEdgeConn(t, e)
	rec.waitFor(t, EventTypeConnectionClosed)

	require.Equal(t,
		[]EventType{EventTypeConnectionOpened, EventTypeUpstreamDialComplete, EventTypeConnectionClosed},
		rec.types())
	dial := eventsOf[*EventUpstreamDialComplete](rec)[0]
	assert.ErrorIs(t, dial.Err, syscall.ECONNREFUSED)
	assert.Same(t, e, dial.Endpoint)

	_, err := edgeClient.Read(make([]byte, 1))
	assert.Equal(t, io.EOF, err, "inbound connection closes after a failed dial")
}

func TestForwarderTLSUpstreamUntrustedCertFailsHandshake(t *testing.T) {
	upstream := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(upstream.Close)

	e, rec := newRecordedForwarder(t, "tls://"+upstream.Listener.Addr().String())
	tcpEndpoint(e)

	startEdgeConn(t, e)
	rec.waitFor(t, EventTypeConnectionClosed)

	require.Equal(t,
		[]EventType{EventTypeConnectionOpened, EventTypeUpstreamDialComplete, EventTypeUpstreamTLSHandshakeComplete, EventTypeConnectionClosed},
		rec.types())
	assert.NoError(t, eventsOf[*EventUpstreamDialComplete](rec)[0].Err)
	handshake := eventsOf[*EventUpstreamTLSHandshakeComplete](rec)[0]
	var certErr *tls.CertificateVerificationError
	assert.ErrorAs(t, handshake.Err, &certErr)
	assert.Same(t, e, handshake.Endpoint)
}

func TestForwarderTLSUpstreamTrustedCertForwardsData(t *testing.T) {
	upstream := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(upstream.Close)

	e, rec := newRecordedForwarder(t, "tls://"+upstream.Listener.Addr().String())
	trusting(e, upstream)
	tcpEndpoint(e)

	edgeClient := startEdgeConn(t, e)
	status := sendRequest(t, edgeClient, bufio.NewReader(edgeClient))
	require.NoError(t, edgeClient.Close())
	rec.waitFor(t, EventTypeConnectionClosed)

	assert.Equal(t, http.StatusNotFound, status)
	require.Equal(t,
		[]EventType{EventTypeConnectionOpened, EventTypeUpstreamDialComplete, EventTypeUpstreamTLSHandshakeComplete, EventTypeConnectionClosed},
		rec.types())
	assert.NoError(t, eventsOf[*EventUpstreamDialComplete](rec)[0].Err)
	assert.NoError(t, eventsOf[*EventUpstreamTLSHandshakeComplete](rec)[0].Err)
}

// The client leaves after the forwarder sends its ClientHello. Closing the
// connection must end a handshake that the upstream never answers.
func TestForwarderStalledTLSUpstreamClosesWhenClientLeaves(t *testing.T) {
	addr, accepted := silentUpstream(t)
	e, rec := newRecordedForwarder(t, "tls://"+addr)
	tcpEndpoint(e)

	edgeClient := startEdgeConn(t, e)
	upstreamConn := <-accepted
	require.NoError(t, upstreamConn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, err := upstreamConn.Read(make([]byte, 1))
	require.NoError(t, err, "the forwarder starts the handshake")
	require.NoError(t, edgeClient.Close())
	rec.waitFor(t, EventTypeConnectionClosed)

	_, err = io.Copy(io.Discard, upstreamConn)
	assert.NoError(t, err, "the forwarder closes the upstream connection")
	require.Equal(t,
		[]EventType{EventTypeConnectionOpened, EventTypeUpstreamDialComplete, EventTypeUpstreamTLSHandshakeComplete, EventTypeConnectionClosed},
		rec.types())
	assert.ErrorIs(t, eventsOf[*EventUpstreamTLSHandshakeComplete](rec)[0].Err, net.ErrClosed)
}

func TestForwarderHTTPEndpointRefusedUpstreamEmitsDialError(t *testing.T) {
	e, rec := newRecordedForwarder(t, "http://"+refusedAddr(t))
	httpsEndpoint(e)

	edgeClient := startEdgeConn(t, e)
	status := sendRequest(t, edgeClient, bufio.NewReader(edgeClient))

	assert.Equal(t, http.StatusBadGateway, status)
	dials := eventsOf[*EventUpstreamDialComplete](rec)
	require.Len(t, dials, 1)
	assert.ErrorIs(t, dials[0].Err, syscall.ECONNREFUSED)
	assert.Empty(t, eventsOf[*EventUpstreamTLSHandshakeComplete](rec))
}

func TestForwarderHTTPSUpstreamUntrustedCertIsBadGateway(t *testing.T) {
	upstream := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(upstream.Close)

	e, rec := newRecordedForwarder(t, upstream.URL)
	httpsEndpoint(e)

	edgeClient := startEdgeConn(t, e)
	status := sendRequest(t, edgeClient, bufio.NewReader(edgeClient))

	assert.Equal(t, http.StatusBadGateway, status)
	dials := eventsOf[*EventUpstreamDialComplete](rec)
	require.Len(t, dials, 1)
	assert.NoError(t, dials[0].Err)
	handshakes := eventsOf[*EventUpstreamTLSHandshakeComplete](rec)
	require.Len(t, handshakes, 1)
	var certErr *tls.CertificateVerificationError
	assert.ErrorAs(t, handshakes[0].Err, &certErr)
}

func TestForwarderHTTPSUpstreamReusesConnectionAndHandshake(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(upstream.Close)

	e, rec := newRecordedForwarder(t, upstream.URL)
	trusting(e, upstream)
	httpsEndpoint(e)

	edgeClient := startEdgeConn(t, e)
	br := bufio.NewReader(edgeClient)
	sendRequest(t, edgeClient, br)
	sendRequest(t, edgeClient, br)

	assert.Len(t, eventsOf[*EventUpstreamDialComplete](rec), 1)
	assert.Len(t, eventsOf[*EventUpstreamTLSHandshakeComplete](rec), 1)
}

// The handshake ends after the request is gone. The trace must still report it.
func TestForwarderHTTPSUpstreamStalledHandshakeReportsCanceled(t *testing.T) {
	addr, accepted := silentUpstream(t)
	e, rec := newRecordedForwarder(t, "https://"+addr)
	httpsEndpoint(e)

	edgeClient := startEdgeConn(t, e)
	_, err := io.WriteString(edgeClient, "GET / HTTP/1.1\r\nHost: example.test\r\n\r\n")
	require.NoError(t, err)
	upstreamConn := <-accepted
	require.NoError(t, upstreamConn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, err = upstreamConn.Read(make([]byte, 1))
	require.NoError(t, err, "the transport starts the handshake")
	require.NoError(t, edgeClient.Close())

	rec.waitFor(t, EventTypeUpstreamTLSHandshakeComplete)
	assert.ErrorIs(t, eventsOf[*EventUpstreamTLSHandshakeComplete](rec)[0].Err, context.Canceled)
}

// With a DialTLSContext, every connection offers h2, and an HTTP/1.1 upgrade to
// an http2 upstream fails. The Transport must keep the handshake.
func TestForwarderHTTPSUpstreamUpgradeUsesHTTP1(t *testing.T) {
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 1 || r.Header.Get("Upgrade") != "websocket" {
			http.Error(w, "want an HTTP/1.1 upgrade", http.StatusBadRequest)
			return
		}
		conn, brw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck
		_, _ = brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		_ = brw.Flush()
	}))
	upstream.EnableHTTP2 = true
	upstream.StartTLS()
	t.Cleanup(upstream.Close)

	e, rec := newRecordedForwarder(t, upstream.URL)
	trusting(e, upstream)
	e.upstreamProtocol = "http2"
	httpsEndpoint(e)

	edgeClient := startEdgeConn(t, e)
	_, err := io.WriteString(edgeClient, "GET / HTTP/1.1\r\nHost: example.test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	require.NoError(t, err)
	resp, err := http.ReadResponse(bufio.NewReader(edgeClient), nil)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	assert.Len(t, eventsOf[*EventUpstreamTLSHandshakeComplete](rec), 1)
}

// slowDialer waits for delay, then dials with a net.Dialer.
type slowDialer struct {
	delay time.Duration
}

func (d slowDialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}

func (d slowDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	time.Sleep(d.delay)
	var nd net.Dialer
	return nd.DialContext(ctx, network, address)
}

// The dial event times the caller's dialer and the handshake event times the
// handshake, so a slow dial shows in one and not the other.
func TestForwarderSlowDialIsNotHandshakeTime(t *testing.T) {
	const delay = 300 * time.Millisecond
	for _, tc := range []struct {
		name     string
		upstream func(*httptest.Server) string
		endpoint func(*endpointForwarder)
	}{
		{"raw", func(s *httptest.Server) string { return "tls://" + s.Listener.Addr().String() }, tcpEndpoint},
		{"http", func(s *httptest.Server) string { return s.URL }, httpsEndpoint},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewTLSServer(http.NotFoundHandler())
			t.Cleanup(upstream.Close)

			e, rec := newRecordedForwarder(t, tc.upstream(upstream))
			trusting(e, upstream)
			e.upstreamDialer = slowDialer{delay: delay}
			tc.endpoint(e)

			edgeClient := startEdgeConn(t, e)
			sendRequest(t, edgeClient, bufio.NewReader(edgeClient))
			rec.waitFor(t, EventTypeUpstreamTLSHandshakeComplete)

			dials := eventsOf[*EventUpstreamDialComplete](rec)
			handshakes := eventsOf[*EventUpstreamTLSHandshakeComplete](rec)
			require.Len(t, dials, 1)
			require.Len(t, handshakes, 1)
			assert.GreaterOrEqual(t, dials[0].Duration, delay)
			assert.Less(t, handshakes[0].Duration, delay)
		})
	}
}
