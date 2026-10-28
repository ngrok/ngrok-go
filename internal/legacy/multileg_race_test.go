package legacy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.ngrok.com/muxado/v2"

	"golang.ngrok.com/ngrok/v2/internal/testcontext"
	"golang.ngrok.com/ngrok/v2/internal/tunnel/proto"
)

// multilegHost is the only name the test certificate is valid for. Connect
// takes the TLS ServerName from the host part of the server address, so every
// leg address uses this host and differs only in its port.
const multilegHost = "connect.ngrok-agent.com"

// multilegServer is an in-memory tunnel server. It is both the Dialer the
// client connects through and the server on the other end of each dial, so no
// connection leaves the process.
//
// It answers exactly two things: the Auth request each leg opens, and the
// Update request the server itself initiates to observe that a leg has reached
// its accept loop. Anything else is reported as a request the fake cannot
// answer, which fails the test rather than hanging it.
type multilegServer struct {
	tlsConfig *tls.Config
	caPool    *x509.CertPool
	addrs     []string

	mu       sync.Mutex
	dialed   []string
	authed   map[uint32]int
	accepted map[uint32]int
	sessions []*muxado.Heartbeat
	closed   bool

	// allAuthed closes once every advertised leg has authenticated.
	allAuthed chan struct{}
	// allAccepted closes once every leg has answered a server-initiated
	// request, which means every leg finished connecting and is serving.
	allAccepted chan struct{}
	// failures carries anything the fake could not answer.
	failures chan error
}

func newMultilegServer(t *testing.T) *multilegServer {
	t.Helper()

	cert, pool := multilegCert(t)

	s := &multilegServer{
		tlsConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
		caPool: pool,
		addrs: []string{
			multilegHost + ":443",
			multilegHost + ":4443",
			multilegHost + ":4444",
		},
		authed:      make(map[uint32]int),
		accepted:    make(map[uint32]int),
		allAuthed:   make(chan struct{}),
		allAccepted: make(chan struct{}),
		failures:    make(chan error, 8),
	}

	t.Cleanup(s.close)
	return s
}

// connectAddresses is the leg list the server reports in the Auth response.
// Three addresses is what makes a multi-leg client want three legs.
func (s *multilegServer) connectAddresses() []proto.ConnectAddress {
	addrs := make([]proto.ConnectAddress, 0, len(s.addrs))
	for i, a := range s.addrs {
		addrs = append(addrs, proto.ConnectAddress{
			Region:     fmt.Sprintf("us-%d", i),
			ServerAddr: a,
		})
	}
	return addrs
}

func (s *multilegServer) Dial(network, address string) (net.Conn, error) {
	return s.DialContext(context.Background(), network, address)
}

// DialContext hands the client one half of a pipe and serves TLS on the other.
func (s *multilegServer) DialContext(_ context.Context, _, address string) (net.Conn, error) {
	s.mu.Lock()
	closed := s.closed
	if !closed {
		s.dialed = append(s.dialed, address)
	}
	s.mu.Unlock()

	if closed {
		return nil, net.ErrClosed
	}

	clientSide, serverSide := net.Pipe()
	go s.serve(serverSide)
	return clientSide, nil
}

func (s *multilegServer) serve(conn net.Conn) {
	// The Heartbeat wrapper answers the client's heartbeat stream itself and
	// passes every other typed stream through.
	sess := muxado.NewHeartbeat(muxado.NewTypedStreamSession(
		muxado.Server(tls.Server(conn, s.tlsConfig), nil)), nil, nil)

	s.mu.Lock()
	closed := s.closed
	if !closed {
		s.sessions = append(s.sessions, sess)
	}
	s.mu.Unlock()
	if closed {
		_ = sess.Close()
		return
	}

	for {
		stream, err := sess.AcceptTypedStream()
		if err != nil {
			return
		}
		go s.handleAuth(sess, stream)
	}
}

// handleAuth answers the one request type a leg sends on its own. Once a leg
// is authenticated the server probes it, so the test can tell when the leg is
// serving rather than guessing.
func (s *multilegServer) handleAuth(sess *muxado.Heartbeat, stream muxado.TypedStream) {
	defer func() { _ = stream.Close() }()

	if reqType := proto.ReqType(stream.StreamType()); reqType != proto.AuthReq {
		s.fail(fmt.Errorf("client opened request type %d, which this fake server cannot answer", reqType))
		return
	}

	var req proto.Auth
	if err := json.NewDecoder(stream).Decode(&req); err != nil {
		s.fail(fmt.Errorf("decoding auth request: %w", err))
		return
	}

	leg := req.Extra.LegNumber
	s.record(s.authed, s.allAuthed, leg)

	resp := proto.AuthResp{
		Version:  req.Version[0],
		ClientID: "cl_multileg",
		Extra: proto.AuthRespExtra{
			Version:          "fake-server",
			Region:           fmt.Sprintf("us-%d", leg),
			Cookie:           fmt.Sprintf("cookie-for-leg-%d", leg),
			AccountName:      "test",
			PlanName:         "test",
			AgentSessionID:   "as_multileg",
			ConnectAddresses: s.connectAddresses(),
		},
	}
	if err := json.NewEncoder(stream).Encode(resp); err != nil {
		s.fail(fmt.Errorf("encoding auth response for leg %d: %w", leg, err))
		return
	}

	go s.probe(sess, leg)
}

// probe sends a request the agent only answers from inside its accept loop.
// A reply means the leg is past connecting, which is when it is safe to close
// the session.
func (s *multilegServer) probe(sess *muxado.Heartbeat, leg uint32) {
	stream, err := sess.OpenTypedStream(muxado.StreamType(proto.UpdateReq))
	if err != nil {
		return // the session is shutting down
	}
	defer func() { _ = stream.Close() }()

	if err := json.NewEncoder(stream).Encode(proto.Update{}); err != nil {
		return
	}

	var resp proto.UpdateResp
	if err := json.NewDecoder(stream).Decode(&resp); err != nil {
		return
	}
	if resp.Error != "" {
		s.fail(fmt.Errorf("leg %d refused the update request: %s", leg, resp.Error))
		return
	}

	s.record(s.accepted, s.allAccepted, leg)
}

// record counts an event for a leg and closes done once every leg has one.
func (s *multilegServer) record(counts map[uint32]int, done chan struct{}, leg uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()

	counts[leg]++
	if len(counts) == len(s.addrs) {
		select {
		case <-done:
		default:
			close(done)
		}
	}
}

func (s *multilegServer) fail(err error) {
	select {
	case s.failures <- err:
	default:
	}
}

// legCounts returns a copy of counts, which is how many times each leg number
// was seen.
func (s *multilegServer) legCounts(counts map[uint32]int) map[uint32]int {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make(map[uint32]int, len(counts))
	for leg, n := range counts {
		out[leg] = n
	}
	return out
}

func (s *multilegServer) dialedAddrs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	addrs := append([]string(nil), s.dialed...)
	sort.Strings(addrs)
	return addrs
}

func (s *multilegServer) close() {
	s.mu.Lock()
	s.closed = true
	sessions := s.sessions
	s.sessions = nil
	s.mu.Unlock()

	for _, sess := range sessions {
		_ = sess.Close()
	}
}

// multilegCert returns a self-signed certificate for multilegHost and a pool
// that trusts it, which is what the client needs for the TLS handshake Connect
// performs on every leg.
func multilegCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: multilegHost},
		DNSNames:              []string{multilegHost},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	parsed, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	pool.AddCert(parsed)

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: parsed}, pool
}

// TestMultiLegConcurrentAuth drives Connect against the in-memory server with
// three legs. The legs are spawned at once and share the AuthExtra and the
// server addresses held in the reconnect closure, so running this under the
// race detector is what covers that closure.
func TestMultiLegConcurrentAuth(t *testing.T) {
	ctx, cancel := context.WithTimeout(testcontext.ForTB(t), 30*time.Second)
	defer cancel()

	srv := newMultilegServer(t)

	sess, err := Connect(ctx,
		WithServer(srv.addrs[0]),
		WithDialer(srv),
		WithCA(srv.caPool),
		WithMultiLeg(true),
		WithAuthtoken("fake-token"),
		// An agent only answers an update request if it has a handler for it.
		WithUpdateHandler(func(context.Context, Session) error { return nil }),
		// Keep the transport quiet, so the only traffic is the handshake.
		WithHeartbeatInterval(time.Hour),
		WithHeartbeatTolerance(time.Hour),
	)
	require.NoError(t, err)

	require.Equal(t, "as_multileg", sess.AgentSessionID())
	require.Equal(t, "us-0", sess.(interface{ Region() string }).Region())

	select {
	case <-srv.allAccepted:
	case err := <-srv.failures:
		t.Fatalf("fake server could not answer the client: %v", err)
	case <-ctx.Done():
		t.Fatalf("timed out with legs authenticated %v and serving %v",
			srv.legCounts(srv.authed), srv.legCounts(srv.accepted))
	}

	// Closing once every leg serves keeps the shutdown ordered. The muxado
	// sessions report their own closure, so the error is not interesting.
	_ = sess.Close()

	select {
	case err := <-srv.failures:
		t.Fatalf("fake server could not answer the client: %v", err)
	default:
	}

	require.Equal(t, map[uint32]int{0: 1, 1: 1, 2: 1}, srv.legCounts(srv.authed),
		"each leg must authenticate exactly once, under its own leg number")
	require.Equal(t, srv.addrs, srv.dialedAddrs(),
		"every advertised leg address must be dialed")
}
