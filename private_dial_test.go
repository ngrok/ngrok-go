package ngrok

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"golang.ngrok.com/ngrok/v2/internal/privatedial"
	pbpd "golang.ngrok.com/ngrok/v2/internal/privatedial/pb"
	"golang.ngrok.com/ngrok/v2/internal/tlstest"
)

// stubServer is a minimal in-process private-dial gateway used for unit
// tests. Tests configure its sessionHandler and dialHandler closures to
// produce the responses they want.
type stubServer struct {
	t        *testing.T
	listener net.Listener
	srv      *http.Server
	rootCAs  *x509.CertPool

	mu         sync.Mutex
	sessionsIn int

	// sessionHandler is invoked after the server has read SessionReq. It
	// must write SessionAck (and any further control frames) and return
	// when done — returning closes the control stream.
	sessionHandler func(req *pbpd.SessionReq, w *flushWriter, body io.Reader)

	// dialHandler is invoked for every /dial request after the server
	// has read DialReq.
	dialHandler func(req *pbpd.DialReq, w http.ResponseWriter, body io.Reader)
}

func newStubServer(t *testing.T) *stubServer {
	t.Helper()
	cert, err := tlstest.CreateCertificate()
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	// cert.Leaf is the template, which lacks the DER bytes needed by the
	// verifier. Reparse from cert.Certificate[0] to get a fully populated
	// *x509.Certificate.
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)

	tlsConf := &tls.Config{
		Certificates: []tls.Certificate{*cert},
		NextProtos:   []string{"h2"},
		MinVersion:   tls.VersionTLS13,
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsConf)
	if err != nil {
		t.Fatalf("tls listen: %v", err)
	}

	ss := &stubServer{
		t:        t,
		listener: ln,
		rootCAs:  roots,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/session", ss.serveSession)
	mux.HandleFunc("/dial", ss.serveDial)
	ss.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := http2.ConfigureServer(ss.srv, &http2.Server{}); err != nil {
		t.Fatalf("h2 configure: %v", err)
	}

	go func() {
		err := ss.srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Logf("stub server: %v", err)
		}
	}()

	t.Cleanup(func() { _ = ss.srv.Close() })

	return ss
}

func (s *stubServer) addr() string { return s.listener.Addr().String() }

func (s *stubServer) clientTLS() *tls.Config {
	return &tls.Config{
		RootCAs:    s.rootCAs,
		MinVersion: tls.VersionTLS13,
	}
}

func (s *stubServer) serveSession(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flusher", http.StatusInternalServerError)
		return
	}

	req := new(pbpd.SessionReq)
	if err := privatedial.ReadFrameForTest(r.Body, req); err != nil {
		http.Error(w, "bad SessionReq: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.sessionsIn++
	s.mu.Unlock()

	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	fw := &flushWriter{w: w, f: flusher}
	if s.sessionHandler != nil {
		s.sessionHandler(req, fw, r.Body)
	} else {
		// Default: send a 0-interval SessionAck (no client ping loop),
		// then hold the stream open until the client closes.
		_ = privatedial.WriteFrameForTest(fw, &pbpd.SessionAck{ServerId: "stub", PingInterval: durationpb.New(0)})
		_, _ = io.Copy(io.Discard, r.Body)
	}
}

func (s *stubServer) serveDial(w http.ResponseWriter, r *http.Request) {
	req := new(pbpd.DialReq)
	if err := privatedial.ReadFrameForTest(r.Body, req); err != nil {
		http.Error(w, "bad DialReq: "+err.Error(), http.StatusBadRequest)
		return
	}
	if s.dialHandler != nil {
		s.dialHandler(req, w, r.Body)
		return
	}
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	_, _ = w.Write([]byte("ok\n"))
}

// flushWriter is an io.Writer that flushes the underlying HTTP/2 response
// on every write, so the client sees each protobuf frame promptly.
type flushWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

func (fw *flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	fw.f.Flush()
	return n, err
}

func writeStubFrame(fw *flushWriter, msg proto.Message) error {
	return privatedial.WriteFrameForTest(fw, msg)
}

func newTestDialer(t *testing.T, s *stubServer, extra ...PrivateDialOption) PrivateDialer {
	t.Helper()
	opts := []PrivateDialOption{
		WithPrivateDialAuthtoken("test-token"),
		WithPrivateDialServer(s.addr()),
		WithPrivateDialTLSConfig(s.clientTLS()),
	}
	opts = append(opts, extra...)
	d, err := NewPrivateDialer(opts...)
	if err != nil {
		t.Fatalf("NewPrivateDialer: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestPrivateDialer_RoundTrip(t *testing.T) {
	s := newStubServer(t)
	s.dialHandler = func(req *pbpd.DialReq, w http.ResponseWriter, body io.Reader) {
		if req.GetHost() != "api.internal" || req.GetPort() != 443 {
			t.Errorf("unexpected dial target: %s:%d", req.GetHost(), req.GetPort())
		}
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		buf := make([]byte, 6)
		if _, err := io.ReadFull(body, buf); err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		_, _ = w.Write(buf)
	}
	d := newTestDialer(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := d.DialContext(ctx, "tcp", "api.internal:443")
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("hello\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 6)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "hello\n" {
		t.Errorf("got %q want %q", buf, "hello\n")
	}
}

func TestPrivateDialer_NotFound(t *testing.T) {
	s := newStubServer(t)
	s.dialHandler = func(req *pbpd.DialReq, w http.ResponseWriter, body io.Reader) {
		w.Header().Set("Ngrok-Error-Code", "ERR_NGROK_706")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("no such endpoint"))
	}
	d := newTestDialer(t, s)
	_, err := d.DialContext(context.Background(), "tcp", "missing.internal:80")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var dns *net.DNSError
	if !errors.As(err, &dns) {
		t.Fatalf("expected *net.DNSError, got %T: %v", err, err)
	}
	if !dns.IsNotFound {
		t.Errorf("DNSError.IsNotFound = false")
	}
	var srv *PrivateDialServerError
	if !errors.As(err, &srv) {
		t.Fatalf("expected *PrivateDialServerError, got %T", err)
	}
	if srv.Code != "ERR_NGROK_706" {
		t.Errorf("ServerError.Code = %q", srv.Code)
	}
	if srv.Status != http.StatusNotFound {
		t.Errorf("ServerError.Status = %d", srv.Status)
	}
}

func TestPrivateDialer_Unavailable(t *testing.T) {
	s := newStubServer(t)
	s.dialHandler = func(req *pbpd.DialReq, w http.ResponseWriter, body io.Reader) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("draining"))
	}
	d := newTestDialer(t, s)
	_, err := d.DialContext(context.Background(), "tcp", "api.internal:443")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Errorf("expected ECONNREFUSED, got %v", err)
	}
}

func TestPrivateDialer_Auth(t *testing.T) {
	s := newStubServer(t)
	s.dialHandler = func(req *pbpd.DialReq, w http.ResponseWriter, body io.Reader) {
		w.WriteHeader(http.StatusUnauthorized)
	}
	d := newTestDialer(t, s)
	_, err := d.DialContext(context.Background(), "tcp", "api.internal:443")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Errorf("expected ECONNREFUSED (auth maps to refused), got %v", err)
	}
}

func TestPrivateDialer_DrainReopens(t *testing.T) {
	s := newStubServer(t)
	s.sessionHandler = func(req *pbpd.SessionReq, w *flushWriter, body io.Reader) {
		_ = writeStubFrame(w, &pbpd.SessionAck{ServerId: "first", PingInterval: durationpb.New(0)})
		_ = writeStubFrame(w, &pbpd.ControlFrame{
			Frame: &pbpd.ControlFrame_PleaseDrain{
				PleaseDrain: &pbpd.PleaseDrain{Reason: "test", GracePeriodSeconds: 1},
			},
		})
		_, _ = io.Copy(io.Discard, body)
	}
	d := newTestDialer(t, s)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := d.DialContext(ctx, "tcp", "api.internal:443"); err != nil {
		t.Fatalf("first dial: %v", err)
	}

	// Give the drain frame a chance to land.
	time.Sleep(150 * time.Millisecond)

	if _, err := d.DialContext(ctx, "tcp", "api.internal:443"); err != nil {
		t.Fatalf("second dial: %v", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionsIn < 2 {
		t.Errorf("expected at least 2 sessions opened, got %d", s.sessionsIn)
	}
}

func TestPrivateDialer_CloseRejectsDial(t *testing.T) {
	s := newStubServer(t)
	d := newTestDialer(t, s)
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := d.DialContext(context.Background(), "tcp", "api.internal:443")
	if err == nil {
		t.Fatal("expected error after Close")
	}
}

func TestPrivateDialer_TransportH3Rejected(t *testing.T) {
	_, err := NewPrivateDialer(
		WithPrivateDialAuthtoken("x"),
		WithPrivateDialTransport(TransportH3),
	)
	if !errors.Is(err, ErrTransportUnavailable) {
		t.Errorf("expected ErrTransportUnavailable, got %v", err)
	}
}

func TestPrivateDialer_AuthtokenRequired(t *testing.T) {
	_, err := NewPrivateDialer()
	if err == nil {
		t.Fatal("expected error when authtoken missing")
	}
}

func TestPrivateDialer_BadAddress(t *testing.T) {
	s := newStubServer(t)
	d := newTestDialer(t, s)
	_, err := d.DialContext(context.Background(), "tcp", "no-port")
	if err == nil {
		t.Fatal("expected error")
	}
	var op *net.OpError
	if !errors.As(err, &op) {
		t.Errorf("expected *net.OpError, got %T", err)
	}
}

func TestStubServer_Addr(t *testing.T) {
	s := newStubServer(t)
	_, portStr, err := net.SplitHostPort(s.addr())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	if _, err := strconv.Atoi(portStr); err != nil {
		t.Fatalf("Atoi port: %v", err)
	}
}
