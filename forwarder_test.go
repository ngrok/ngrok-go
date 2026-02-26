package ngrok

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- readProxyProtocolHeader unit tests ---

func TestReadProxyProtocolHeaderV1(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "tcp4",
			input: "PROXY TCP4 192.168.1.1 192.168.1.2 1000 2000\r\n",
			want:  "PROXY TCP4 192.168.1.1 192.168.1.2 1000 2000\r\n",
		},
		{
			name:  "tcp6",
			input: "PROXY TCP6 ::1 ::1 1234 5678\r\npayload",
			want:  "PROXY TCP6 ::1 ::1 1234 5678\r\n",
		},
		{
			name:    "too long – no CRLF within 108 bytes",
			input:   "P" + string(make([]byte, 200)), // no \r\n
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bytes.NewReader([]byte(tt.input))
			got, err := readProxyProtocolHeader(r)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []byte(tt.want), got)
		})
	}
}

func TestReadProxyProtocolHeaderV1_PayloadIntact(t *testing.T) {
	header := "PROXY TCP4 10.0.0.1 10.0.0.2 50000 80\r\n"
	payload := "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"
	r := bytes.NewReader([]byte(header + payload))

	got, err := readProxyProtocolHeader(r)
	require.NoError(t, err)
	assert.Equal(t, []byte(header), got)

	// Remaining bytes in r should be exactly the payload.
	rest, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, []byte(payload), rest)
}

func TestReadProxyProtocolHeaderV2(t *testing.T) {
	// Construct a valid PROXY v2 header: LOCAL command, TCP4 family.
	sig := []byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A}
	verCmd := byte(0x21) // version 2, PROXY command
	fam := byte(0x11)    // INET (IPv4) + STREAM (TCP)
	addrData := []byte{
		192, 168, 1, 1, // src IP
		192, 168, 1, 2, // dst IP
		0x04, 0xD2, // src port 1234
		0x00, 0x50, // dst port 80
	}
	lenBuf := [2]byte{}
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(addrData)))

	var header []byte
	header = append(header, sig...)
	header = append(header, verCmd, fam)
	header = append(header, lenBuf[:]...)
	header = append(header, addrData...)

	payload := []byte("some TLS ClientHello bytes")
	r := bytes.NewReader(append(header, payload...))

	got, err := readProxyProtocolHeader(r)
	require.NoError(t, err)
	assert.Equal(t, header, got)

	rest, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, payload, rest)
}

func TestReadProxyProtocolHeaderUnknown(t *testing.T) {
	r := bytes.NewReader([]byte("\x00invalid"))
	_, err := readProxyProtocolHeader(r)
	require.Error(t, err)
}

// --- end-to-end: PROXY header delivered before TLS ---

// TestConnectToBackendWritesProxyHeaderBeforeTLS verifies that when
// connectToBackend is given a non-nil proxyHeader and the upstream uses TLS,
// the header bytes arrive at the backend as plaintext before the TLS handshake.
func TestConnectToBackendWritesProxyHeaderBeforeTLS(t *testing.T) {
	cert, pool := selfSignedCert(t)

	// Local TLS backend: read raw bytes first (expecting PROXY header), then
	// complete the TLS handshake.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	proxyHeader := []byte("PROXY TCP4 1.2.3.4 5.6.7.8 1000 443\r\n")
	receivedHeader := make(chan []byte, 1)
	serverErr := make(chan error, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()

		// Read exactly as many bytes as the PROXY header.
		buf := make([]byte, len(proxyHeader))
		if _, err := io.ReadFull(conn, buf); err != nil {
			serverErr <- err
			return
		}
		receivedHeader <- buf

		// Now complete the TLS handshake.
		tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
		if err := tlsConn.Handshake(); err != nil {
			serverErr <- err
			return
		}
		tlsConn.Close()
		serverErr <- nil
	}()

	parsed, err := url.Parse("tls://" + ln.Addr().String())
	require.NoError(t, err)

	fwd := &endpointForwarder{
		upstreamURL: *parsed,
		upstreamTLSClientConfig: &tls.Config{
			RootCAs:    pool,
			ServerName: "127.0.0.1",
		},
		proxyProtocol: ProxyProtoV1,
	}

	backendConn, err := fwd.connectToBackend(t.Context(), proxyHeader)
	require.NoError(t, err)
	// Trigger the TLS handshake so the server goroutine can complete it.
	require.NoError(t, backendConn.(*tls.Conn).Handshake())
	backendConn.Close()

	select {
	case got := <-receivedHeader:
		assert.Equal(t, proxyHeader, got)
	case err := <-serverErr:
		require.NoError(t, err, "backend server error before sending header")
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for backend to receive proxy header")
	}

	select {
	case err := <-serverErr:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for backend server goroutine to finish")
	}
}

// selfSignedCert creates a self-signed TLS certificate and returns the
// tls.Certificate and a CertPool containing the certificate's CA.
func selfSignedCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)
	return cert, pool
}
