package integration_tests

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.ngrok.com/ngrok/v2"
)

// SkipIfOffline skips the test if NGROK_TEST_ONLINE environment variable is not set
func SkipIfOffline(t *testing.T) {
	if os.Getenv("NGROK_TEST_ONLINE") == "" {
		t.Skip("Skipping online test because NGROK_TEST_ONLINE is not set")
	}
}

// SetupAgent creates and connects a new agent for testing
func SetupAgent(t *testing.T) (ngrok.Agent, context.Context, context.CancelFunc) {
	// Skip if not running online tests
	SkipIfOffline(t)

	// Get authentication token from environment
	authToken := os.Getenv("NGROK_AUTHTOKEN")
	require.NotEmpty(t, authToken, "NGROK_AUTHTOKEN environment variable is required but not set")

	// Create a new agent for each test
	agent, err := ngrok.NewAgent(
		ngrok.WithAuthtoken(authToken),
	)
	require.NoError(t, err, "Failed to create agent")

	// Start a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	// Connect the agent
	err = agent.Connect(ctx)
	require.NoError(t, err, "Failed to connect agent")

	return agent, ctx, cancel
}

// SetupListener sets up an ngrok listener with the specified options
func SetupListener(t *testing.T, agent ngrok.Agent, ctx context.Context, opts ...ngrok.EndpointOption) ngrok.EndpointListener {
	// Create a listener endpoint
	listener, err := agent.Listen(ctx, opts...)
	require.NoError(t, err, "Failed to create listener")

	// Get the URL of the endpoint
	endpointURL := listener.URL().String()
	t.Logf("Endpoint URL: %s", endpointURL)

	return listener
}

// MakeHTTPRequest makes an HTTP request to the specified URL with the given message
func MakeHTTPRequest(t *testing.T, ctx context.Context, url string, message string) *http.Response {
	// Create a custom transport that doesn't reuse connections
	transport := &http.Transport{
		DisableKeepAlives: true,
	}

	// Create a client with the custom transport
	client := &http.Client{Transport: transport}

	// Make the request
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(message))
	require.NoError(t, err, "Failed to create request")

	t.Logf("Making HTTP request to %s", url)
	resp, err := client.Do(req)
	require.NoError(t, err, "Failed to send request")

	return resp
}

// WaitForForwarderReady polls the forwarder endpoint until it responds or times out
func WaitForForwarderReady(t *testing.T, url string) {
	client := &http.Client{Timeout: 100 * time.Millisecond}
	for start := time.Now(); time.Since(start) < 500*time.Millisecond; {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("Forwarder endpoint didn't become ready in expected time, continuing anyway")
}

// CreateTestCertificate creates a certificate for testing
func CreateTestCertificate(t *testing.T) *tls.Certificate {
	// Generate a self-signed certificate for testing
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err, "Failed to generate private key")

	templ := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &templ, &templ, &privKey.PublicKey, privKey)
	require.NoError(t, err, "Failed to create certificate")

	cert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  privKey,
	}

	return &cert
}

// MakeTCPConnection establishes a TCP connection to the given address
func MakeTCPConnection(t *testing.T, ctx context.Context, address string) (io.ReadWriteCloser, error) {
	t.Helper()
	// Use a simple net.Dialer to connect to the TCP address
	dialer := &net.Dialer{
		Timeout: 500 * time.Millisecond,
	}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// HandleHTTPRequest processes an HTTP request from a connection and sends a response
func HandleHTTPRequest(t *testing.T, conn net.Conn) (string, error) {
	t.Helper()
	// Create a buffered reader for the connection
	reader := bufio.NewReader(conn)

	// Read the HTTP request
	request, err := http.ReadRequest(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read HTTP request: %w", err)
	}

	// Read the request body
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read request body: %w", err)
	}
	message := string(body)

	// Send a response
	response := http.Response{
		StatusCode: http.StatusOK,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
	}
	response.Header.Set("Content-Type", "text/plain")
	response.Body = io.NopCloser(strings.NewReader("Request received"))

	if err := response.Write(conn); err != nil {
		return message, fmt.Errorf("failed to write response: %w", err)
	}

	return message, nil
}

// HandleTCPConnection reads data from a TCP connection and sends a response
func HandleTCPConnection(t *testing.T, conn io.ReadWriteCloser) (string, error) {
	t.Helper()
	// Read data from the connection
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return "", fmt.Errorf("failed to read data: %w", err)
	}

	message := string(buf[:n])

	// Send a response
	response := "Message received"
	if _, err := conn.Write([]byte(response)); err != nil {
		return message, fmt.Errorf("failed to write response: %w", err)
	}

	return message, nil
}

// HandleTLSConnection handles a TLS server connection
func HandleTLSConnection(t *testing.T, conn net.Conn, cert *tls.Certificate) (string, error) {
	t.Helper()
	// Create TLS configuration for server
	config := &tls.Config{
		Certificates: []tls.Certificate{*cert},
	}

	// Create a TLS server connection
	tlsConn := tls.Server(conn, config)
	defer tlsConn.Close()

	// Perform TLS handshake
	if err := tlsConn.Handshake(); err != nil {
		return "", fmt.Errorf("TLS handshake failed: %w", err)
	}

	// Read data from the TLS connection
	buffer := make([]byte, 1024)
	n, err := tlsConn.Read(buffer)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("error reading from TLS connection: %w", err)
	}

	message := ""
	if n > 0 {
		message = string(buffer[:n])

		// Send a response back to the client over TLS
		response := "TLS message received"
		if _, err := tlsConn.Write([]byte(response)); err != nil {
			return message, fmt.Errorf("failed to write TLS response: %w", err)
		}
	}

	return message, nil
}
