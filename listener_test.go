package ngrok

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"
)

// createTestCertificate creates a self-signed certificate for testing
func createTestCertificate(t *testing.T) *tls.Certificate {
	// Generate a private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	// Create a certificate template
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour * 24), // Valid for 24 hours
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Create a self-signed certificate
	certBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	// Create a TLS certificate
	cert := &tls.Certificate{
		Certificate: [][]byte{certBytes},
		PrivateKey:  privateKey,
		Leaf:        &template,
	}

	return cert
}

// TestWrapConnWithTLS tests the TLS connection wrapper
func TestWrapConnWithTLS(t *testing.T) {
	// Create a test certificate
	cert := createTestCertificate(t)

	// Create a pipe for testing
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close() //nolint:errcheck
	defer clientConn.Close() //nolint:errcheck

	// Apply TLS in a separate goroutine
	go func() {
		// Configure TLS client
		config := &tls.Config{
			InsecureSkipVerify: true, // Skip verification for test
		}

		// Create TLS client connection
		clientTLS := tls.Client(clientConn, config)

		// Perform handshake - this will happen when we first use the connection
		_, err := clientTLS.Write([]byte("hello"))
		if err != nil {
			t.Errorf("Failed to write to TLS connection: %v", err)
			return
		}

		// Close the client connection
		_ = clientTLS.Close()
	}()

	// Wrap the server connection with TLS
	config := &tls.Config{
		Certificates: []tls.Certificate{*cert},
	}
	serverTLS := wrapConnWithTLS(serverConn, config)

	// Read the test data - this will trigger the handshake on first use
	buf := make([]byte, 10)
	n, err := serverTLS.Read(buf)
	if err != nil {
		t.Fatalf("Failed to read from TLS connection: %v", err)
	}

	// Verify the data
	if string(buf[:n]) != "hello" {
		t.Fatalf("Unexpected data: %s", string(buf[:n]))
	}
}

// TestWrapConnWithTLSNil tests that wrapConnWithTLS returns the original connection when no certificate is provided
func TestWrapConnWithTLSNil(t *testing.T) {
	// Create a pipe for testing
	conn1, conn2 := net.Pipe()
	defer conn1.Close() //nolint:errcheck
	defer conn2.Close() //nolint:errcheck

	// Call wrapConnWithTLS with nil config
	result := wrapConnWithTLS(conn1, nil)

	// Verify that the original connection was returned
	if result != conn1 {
		t.Fatalf("wrapConnWithTLS didn't return the original connection")
	}
}
