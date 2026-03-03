package ngrok

import (
	"crypto/tls"
	"net"
	"testing"

	"golang.ngrok.com/ngrok/v2/internal/tlstest"
)

// TestWrapConnWithTLS tests the TLS connection wrapper
func TestWrapConnWithTLS(t *testing.T) {
	// Create a test certificate
	cert, err := tlstest.CreateCertificate()
	if err != nil {
		t.Fatal(err)
	}

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
