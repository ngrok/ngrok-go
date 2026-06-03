package privatedial

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"golang.org/x/net/http2"
	"google.golang.org/protobuf/encoding/protodelim"

	pbpd "golang.ngrok.com/ngrok/privatedial/pb_private_dial"
)

// These tests stand up real HTTP/2 and HTTP/3 servers that speak the
// private-dial /session and /dial handshakes, then drive the actual client
// against them. They exercise the Happy-Eyeballs transport selection end to
// end: which protocol wins the race, the staggered HTTP/2 fallback when QUIC
// is unreachable, process-sticky reuse, ForceProtocol, and a full duplex
// /dial echo over each transport.
//
// The race relies on process-global sticky state, so these tests reset it
// between cases and must not run in parallel.

// resetSticky clears the process-global protocol decision so each test
// starts from ProtocolAuto.
func resetSticky() {
	stickyMu.Lock()
	stickyProtocol = ProtocolAuto
	stickyMu.Unlock()
}

func TestRaceQUICWins(t *testing.T) {
	resetSticky()
	cert := genTLSCert(t)
	var quicHits, h2Hits atomic.Int64
	quicAddr := startH3Server(t, cert, privateDialHandler(&quicHits))
	h2Addr := startH2Server(t, cert, privateDialHandler(&h2Hits))

	sess := mustOpenSession(t, ClientOpts{
		QUICServerAddr: quicAddr,
		H2ServerAddr:   h2Addr,
		AuthToken:      "test-token",
		TLSConfig:      &tls.Config{InsecureSkipVerify: true},
	})
	defer sess.Close()

	if got := getStickyProtocol(); got != ProtocolQUIC {
		t.Fatalf("expected QUIC to win the race, sticky protocol = %v", got)
	}
	// QUIC connects on localhost well inside the 250ms head start, so the
	// HTTP/2 attempt should never be launched.
	if n := h2Hits.Load(); n != 0 {
		t.Fatalf("expected no HTTP/2 connection when QUIC wins fast, got %d hits", n)
	}
	if quicHits.Load() == 0 {
		t.Fatal("expected the QUIC server to receive the /session request")
	}
}

func TestRaceFallsBackToH2(t *testing.T) {
	resetSticky()
	cert := genTLSCert(t)
	var h2Hits atomic.Int64
	// QUIC points at a UDP socket that silently drops everything, so the
	// QUIC handshake never completes and the race must stagger in HTTP/2.
	quicAddr := blackholeUDPAddr(t)
	h2Addr := startH2Server(t, cert, privateDialHandler(&h2Hits))

	start := time.Now()
	sess := mustOpenSession(t, ClientOpts{
		QUICServerAddr: quicAddr,
		H2ServerAddr:   h2Addr,
		AuthToken:      "test-token",
		TLSConfig:      &tls.Config{InsecureSkipVerify: true},
	})
	defer sess.Close()

	if got := getStickyProtocol(); got != ProtocolH2 {
		t.Fatalf("expected HTTP/2 to win when QUIC is unreachable, sticky protocol = %v", got)
	}
	if h2Hits.Load() == 0 {
		t.Fatal("expected the HTTP/2 server to receive the /session request")
	}
	// The HTTP/2 attempt is staggered behind the QUIC head start, so the
	// session can't have come up before quicHeadStart elapsed.
	if elapsed := time.Since(start); elapsed < quicHeadStart {
		t.Fatalf("fallback completed in %v, expected at least the %v head start", elapsed, quicHeadStart)
	}
}

func TestStickyProtocolReused(t *testing.T) {
	resetSticky()
	cert := genTLSCert(t)
	var quicHits, h2Hits atomic.Int64
	quicAddr := startH3Server(t, cert, privateDialHandler(&quicHits))
	h2Addr := startH2Server(t, cert, privateDialHandler(&h2Hits))

	opts := ClientOpts{
		QUICServerAddr: quicAddr,
		H2ServerAddr:   h2Addr,
		AuthToken:      "test-token",
		TLSConfig:      &tls.Config{InsecureSkipVerify: true},
	}

	// First session races and settles on QUIC.
	sess1 := mustOpenSession(t, opts)
	defer sess1.Close()
	if got := getStickyProtocol(); got != ProtocolQUIC {
		t.Fatalf("first session should settle on QUIC, sticky = %v", got)
	}

	// A second session must reuse the sticky choice without racing, so
	// HTTP/2 is still never touched.
	sess2 := mustOpenSession(t, opts)
	defer sess2.Close()
	if n := h2Hits.Load(); n != 0 {
		t.Fatalf("sticky reuse should not touch HTTP/2, got %d hits", n)
	}
	if quicHits.Load() < 2 {
		t.Fatalf("expected both sessions to use QUIC, got %d QUIC hits", quicHits.Load())
	}
}

func TestForceProtocol(t *testing.T) {
	cert := genTLSCert(t)

	t.Run("h2", func(t *testing.T) {
		resetSticky()
		var quicHits, h2Hits atomic.Int64
		quicAddr := startH3Server(t, cert, privateDialHandler(&quicHits))
		h2Addr := startH2Server(t, cert, privateDialHandler(&h2Hits))

		sess := mustOpenSession(t, ClientOpts{
			QUICServerAddr: quicAddr,
			H2ServerAddr:   h2Addr,
			AuthToken:      "test-token",
			ForceProtocol:  ProtocolH2,
			TLSConfig:      &tls.Config{InsecureSkipVerify: true},
		})
		defer sess.Close()

		if h2Hits.Load() == 0 {
			t.Fatal("ForceProtocol=H2 should use the HTTP/2 server")
		}
		if quicHits.Load() != 0 {
			t.Fatalf("ForceProtocol=H2 must not touch QUIC, got %d hits", quicHits.Load())
		}
		// Forcing a protocol bypasses the race and must not write sticky state.
		if got := getStickyProtocol(); got != ProtocolAuto {
			t.Fatalf("ForceProtocol should not set sticky state, got %v", got)
		}
	})

	t.Run("quic", func(t *testing.T) {
		resetSticky()
		var quicHits, h2Hits atomic.Int64
		quicAddr := startH3Server(t, cert, privateDialHandler(&quicHits))
		h2Addr := startH2Server(t, cert, privateDialHandler(&h2Hits))

		sess := mustOpenSession(t, ClientOpts{
			QUICServerAddr: quicAddr,
			H2ServerAddr:   h2Addr,
			AuthToken:      "test-token",
			ForceProtocol:  ProtocolQUIC,
			TLSConfig:      &tls.Config{InsecureSkipVerify: true},
		})
		defer sess.Close()

		if quicHits.Load() == 0 {
			t.Fatal("ForceProtocol=QUIC should use the QUIC server")
		}
		if h2Hits.Load() != 0 {
			t.Fatalf("ForceProtocol=QUIC must not touch HTTP/2, got %d hits", h2Hits.Load())
		}
	})
}

// TestDialEcho proves a full-duplex /dial stream works over each transport:
// bytes written to the returned net.Conn are echoed back by the server. This
// validates that the HTTP/3 path streams the request body concurrently with
// the response, which the protocol depends on.
func TestDialEcho(t *testing.T) {
	cert := genTLSCert(t)

	for _, tc := range []struct {
		name  string
		proto Protocol
	}{
		{"quic", ProtocolQUIC},
		{"h2", ProtocolH2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetSticky()
			var hits atomic.Int64
			quicAddr := startH3Server(t, cert, privateDialHandler(&hits))
			h2Addr := startH2Server(t, cert, privateDialHandler(&hits))

			sess := mustOpenSession(t, ClientOpts{
				QUICServerAddr: quicAddr,
				H2ServerAddr:   h2Addr,
				AuthToken:      "test-token",
				ForceProtocol:  tc.proto,
				TLSConfig:      &tls.Config{InsecureSkipVerify: true},
			})
			defer sess.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, err := sess.DialContext(ctx, "tcp", "foo.private:80")
			if err != nil {
				t.Fatalf("DialContext: %v", err)
			}
			defer conn.Close()

			payload := []byte("hello private dial over " + tc.name)
			if _, err := conn.Write(payload); err != nil {
				t.Fatalf("Write: %v", err)
			}
			// Signal end-of-request so the server's echo copy returns.
			if err := conn.(*dialConn).CloseWrite(); err != nil {
				t.Fatalf("CloseWrite: %v", err)
			}

			got, err := io.ReadAll(conn)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if string(got) != string(payload) {
				t.Fatalf("echo mismatch: got %q want %q", got, payload)
			}
		})
	}
}

// mustOpenSession opens a session with the given opts, failing the test on
// error.
func mustOpenSession(t *testing.T, opts ClientOpts) *Session {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess, err := NewClient(opts).OpenSession(ctx)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	return sess
}

// privateDialHandler returns a handler that speaks the private-dial protocol:
// /session reads a SessionReq and replies with a SessionAck, holding the
// stream open; /dial reads a DialReq and then echoes the raw stream. counter
// is incremented once per request so tests can assert which transport was
// used.
func privateDialHandler(counter *atomic.Int64) http.Handler {
	unmarshal := &protodelim.UnmarshalOptions{MaxSize: 16 * 1024}
	mux := http.NewServeMux()

	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		counter.Add(1)
		var req pbpd.SessionReq
		if err := unmarshal.UnmarshalFrom(bufio.NewReader(r.Body), &req); err != nil {
			http.Error(w, "bad SessionReq", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		if _, err := protodelim.MarshalTo(w, &pbpd.SessionAck{ServerId: "test-server"}); err != nil {
			return
		}
		flush(w)
		// Keep the control stream open for the life of the session.
		<-r.Context().Done()
	})

	mux.HandleFunc("/dial", func(w http.ResponseWriter, r *http.Request) {
		counter.Add(1)
		br := bufio.NewReader(r.Body)
		var dreq pbpd.DialReq
		if err := unmarshal.UnmarshalFrom(br, &dreq); err != nil {
			http.Error(w, "bad DialReq", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		flush(w)
		// The stream is now raw TCP: echo everything the client sends.
		// br may hold bytes already read past the DialReq, so copy from it.
		_, _ = io.Copy(flushWriter{w}, br)
	})

	return mux
}

func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// flushWriter flushes after every write so echoed bytes reach the client
// without waiting for the handler to return.
type flushWriter struct{ w http.ResponseWriter }

func (fw flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	flush(fw.w)
	return n, err
}

// startH2Server starts an HTTP/2-over-TLS server on a loopback port and
// returns its address.
func startH2Server(t *testing.T, cert tls.Certificate, h http.Handler) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	srv := &http.Server{
		Handler: h,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{"h2"},
			MinVersion:   tls.VersionTLS13,
		},
	}
	if err := http2.ConfigureServer(srv, &http2.Server{}); err != nil {
		t.Fatalf("ConfigureServer: %v", err)
	}
	go srv.Serve(tls.NewListener(ln, srv.TLSConfig))
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}

// startH3Server starts an HTTP/3 (QUIC) server on a loopback UDP port and
// returns its address.
func startH3Server(t *testing.T, cert tls.Certificate, h http.Handler) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	srv := &http3.Server{
		Handler: h,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS13,
		},
		QUICConfig: &quic.Config{},
	}
	go srv.Serve(pc)
	t.Cleanup(func() {
		_ = srv.Close()
		_ = pc.Close()
	})
	return pc.LocalAddr().String()
}

// blackholeUDPAddr returns the address of a UDP socket that silently reads and
// discards everything, so a QUIC handshake against it never completes (and
// produces no ICMP unreachable that would fail the dial early).
func blackholeUDPAddr(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	go func() {
		buf := make([]byte, 2048)
		for {
			if _, _, err := pc.ReadFrom(buf); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { _ = pc.Close() })
	return pc.LocalAddr().String()
}

// genTLSCert generates a short-lived self-signed certificate for loopback use.
func genTLSCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
