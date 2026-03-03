package integration_tests

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.ngrok.com/ngrok/v2"
	"golang.ngrok.com/ngrok/v2/internal/testutil"
)

// parseProxyProtocolHeader extracts client and server information from a PROXY protocol header.
func parseProxyProtocolHeader(reader *bufio.Reader) (srcAddr, dstAddr net.Addr, err error) {
	// Read the first line from the connection
	header, err := reader.ReadString('\n')
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read PROXY header: %v", err)
	}

	// Trim trailing newline
	header = strings.TrimSuffix(header, "\r\n")
	header = strings.TrimSuffix(header, "\n")

	// Split the header into parts
	parts := strings.Split(header, " ")
	if len(parts) < 6 || parts[0] != "PROXY" {
		return nil, nil, fmt.Errorf("invalid PROXY protocol header: %s", header)
	}

	// Extract information
	proto := parts[1]   // TCP4 or TCP6
	srcIP := parts[2]   // Source IP
	dstIP := parts[3]   // Destination IP
	srcPort := parts[4] // Source port
	dstPort := parts[5] // Destination port

	// Parse ports
	srcPortInt, err := strconv.Atoi(srcPort)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid source port: %v", err)
	}
	dstPortInt, err := strconv.Atoi(dstPort)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid destination port: %v", err)
	}

	// Create addresses
	if proto == "TCP4" {
		srcAddr = &net.TCPAddr{IP: net.ParseIP(srcIP), Port: srcPortInt}
		dstAddr = &net.TCPAddr{IP: net.ParseIP(dstIP), Port: dstPortInt}
	} else if proto == "TCP6" {
		srcAddr = &net.TCPAddr{IP: net.ParseIP(srcIP), Port: srcPortInt}
		dstAddr = &net.TCPAddr{IP: net.ParseIP(dstIP), Port: dstPortInt}
	} else {
		return nil, nil, fmt.Errorf("unsupported protocol: %s", proto)
	}

	return srcAddr, dstAddr, nil
}

// bufferedConn combines a net.Conn with a bufio.Reader to implement net.Conn interface.
type bufferedConn struct {
	r *bufio.Reader
	c net.Conn
}

func (b *bufferedConn) Read(p []byte) (int, error) {
	return b.r.Read(p)
}

func (b *bufferedConn) Write(p []byte) (int, error) {
	return b.c.Write(p)
}

func (b *bufferedConn) Close() error {
	return b.c.Close()
}

// Required net.Conn interface methods that delegate to the underlying connection
func (b *bufferedConn) LocalAddr() net.Addr {
	return b.c.LocalAddr()
}

func (b *bufferedConn) RemoteAddr() net.Addr {
	return b.c.RemoteAddr()
}

func (b *bufferedConn) SetDeadline(t time.Time) error {
	return b.c.SetDeadline(t)
}

func (b *bufferedConn) SetReadDeadline(t time.Time) error {
	return b.c.SetReadDeadline(t)
}

func (b *bufferedConn) SetWriteDeadline(t time.Time) error {
	return b.c.SetWriteDeadline(t)
}

// verifyClientAddr checks that the client address received via PROXY protocol is valid.
func verifyClientAddr(t *testing.T, clientAddr net.Addr) {
	require.NotNil(t, clientAddr, "Client address should not be nil")

	t.Logf("Received client address via PROXY protocol: %s", clientAddr.String())

	// We can't verify exact IP matches in a public test environment,
	// but we can verify that something reasonable came through
	tcpAddr, ok := clientAddr.(*net.TCPAddr)
	assert.True(t, ok, "Expected TCP address, got %T", clientAddr)
	if !ok {
		return
	}

	// Log the client IP for manual verification
	t.Logf("Client IP via PROXY protocol: %s", tcpAddr.IP.String())

	// If we're testing locally, this might be a loopback, but in CI it should be a public IP
	// For this test, we just verify we got something non-nil
	assert.False(t, tcpAddr.IP == nil || tcpAddr.IP.String() == "", "Expected non-empty IP address")
}

// handleTLSConnection handles a TLS connection with PROXY protocol already read
func handleTLSConnection(t *testing.T, conn net.Conn, reader *bufio.Reader, srcAddr net.Addr) {
	// Create a server TLS certificate for the handshake
	servCert := CreateTestCertificate(t)

	// Create TLS configuration for server
	config := &tls.Config{
		Certificates: []tls.Certificate{*servCert},
	}

	// Use the remaining buffer as the source for the TLS connection
	// Create a TLS server connection
	tlsConn := tls.Server(&bufferedConn{reader, conn}, config)
	defer tlsConn.Close()

	// Perform TLS handshake
	if err := tlsConn.Handshake(); err != nil {
		t.Logf("TLS handshake failed: %v", err)
		return
	}

	// Read data from the TLS connection
	buffer := make([]byte, 1024)
	n, err := tlsConn.Read(buffer)
	if err != nil && err != io.EOF {
		assert.NoError(t, err, "Error reading from TLS connection")
		return
	}

	if n > 0 {
		clientMsg := string(buffer[:n])
		t.Logf("Received client message over TLS: %s", clientMsg)

		// Send a response back to the client over TLS
		response := fmt.Sprintf("Received data with PROXY protocol from %s over TLS", srcAddr)
		_, err := tlsConn.Write([]byte(response))
		assert.NoError(t, err, "Failed to write TLS response")
	}
}

// handleHTTPConnection handles an HTTP connection with PROXY protocol already read
func handleHTTPConnection(t *testing.T, conn net.Conn, reader *bufio.Reader, srcAddr net.Addr) {
	// Read plaintext after the PROXY header
	buffer := make([]byte, 1024)
	n, err := reader.Read(buffer)
	if err != nil && err != io.EOF {
		assert.NoError(t, err, "Error reading from connection")
		return
	}

	if n > 0 {
		clientMsg := string(buffer[:n])
		t.Logf("Received client message: %s", clientMsg)

		// For HTTP/HTTPS endpoints, send a proper HTTP response
		message := fmt.Sprintf("Received data with PROXY protocol from %s", srcAddr)
		response := fmt.Sprintf(
			"HTTP/1.1 200 OK\r\n"+
				"Content-Type: text/plain\r\n"+
				"Content-Length: %d\r\n"+
				"Connection: close\r\n"+
				"\r\n"+
				"%s",
			len(message), message)
		_, err := conn.Write([]byte(response))
		assert.NoError(t, err, "Failed to write HTTP response")
		t.Logf("Sent HTTP response with status 200 OK")
	}
}

// handleTCPConnection handles a TCP connection with PROXY protocol already read
func handleTCPConnection(t *testing.T, conn net.Conn, reader *bufio.Reader, srcAddr net.Addr) {
	// Read plaintext after the PROXY header
	buffer := make([]byte, 1024)
	n, err := reader.Read(buffer)
	if err != nil && err != io.EOF {
		assert.NoError(t, err, "Error reading from connection")
		return
	}

	if n > 0 {
		clientMsg := string(buffer[:n])
		t.Logf("Received client message: %s", clientMsg)

		// For TCP endpoints, send plain text response
		response := fmt.Sprintf("Received data with PROXY protocol from %s", srcAddr)
		_, err := conn.Write([]byte(response))
		assert.NoError(t, err, "Failed to write response")
	}
}

// connectHTTPSClient connects to an HTTPS endpoint using an HTTP client
func connectHTTPSClient(ctx context.Context, t *testing.T, endpointURL string) {
	// Use MakeHTTPRequest to send test message
	message := "Test message for PROXY protocol"
	resp := MakeHTTPRequest(t, ctx, endpointURL, message)
	defer resp.Body.Close()

	// Read the response
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")
	t.Logf("Response: %s", string(respBody))
}

// connectTCPClient connects to a TCP endpoint using a direct TCP connection
func connectTCPClient(ctx context.Context, t *testing.T, endpointURL string) {
	// For TCP, use direct TCP connection
	u, err := url.Parse(endpointURL)
	require.NoError(t, err, "Failed to parse URL")

	// Connect to the endpoint using MakeTCPConnection
	clientConn, err := MakeTCPConnection(t, ctx, u.Host)
	require.NoError(t, err, "Failed to connect to TCP endpoint")
	defer clientConn.Close()

	// Send test message
	testMessage := "Test message for PROXY protocol"
	_, err = clientConn.Write([]byte(testMessage))
	require.NoError(t, err, "Failed to send data")

	// Read response
	buffer := make([]byte, 1024)
	n, err := clientConn.Read(buffer)
	require.NoError(t, err, "Failed to read response")
	response := string(buffer[:n])
	t.Logf("Received response: %s", response)
}

// connectTLSClient connects to a TLS endpoint using a TLS client
func connectTLSClient(t *testing.T, endpointURL string) {
	// For TLS, use TLS client
	u, err := url.Parse(endpointURL)
	require.NoError(t, err, "Failed to parse URL")

	// Make sure we have a port
	host := u.Host
	if !strings.Contains(host, ":") {
		host = host + ":443"
	}

	// Connect using TLS as required for TLS endpoints
	config := &tls.Config{
		InsecureSkipVerify: true, // Skip verification for testing
	}

	// Establish a proper TLS connection
	clientConn, err := tls.Dial("tcp", host, config)
	require.NoError(t, err, "Failed to connect with TLS")
	defer clientConn.Close()

	// Send test message over the TLS connection
	testMessage := "Test message for PROXY protocol TLS endpoint"
	_, err = clientConn.Write([]byte(testMessage))
	require.NoError(t, err, "Failed to send data over TLS")

	// Read response
	buffer := make([]byte, 1024)
	n, err := clientConn.Read(buffer)
	require.NoError(t, err, "Failed to read response from TLS connection")
	response := string(buffer[:n])
	t.Logf("Received response from TLS endpoint: %s", response)
}

// TestProxyProtoIntegration tests PROXY protocol functionality with each supported protocol
func TestProxyProtoIntegration(t *testing.T) {
	// Skip if not running online tests
	SkipIfOffline(t)

	// Define the schemes to test
	// Test TCP, TLS, and HTTPS with PROXY protocol
	// Note: For HTTPS endpoints, ngrok terminates TLS at their edge, so our listener receives plain HTTP
	schemes := []string{"tcp://", "tls://", "https://"}

	for _, s := range schemes {
		// Create a subtest for each scheme
		scheme := s // Local copy to avoid loop variable capture
		t.Run(scheme, func(t *testing.T) {
			// Mark this test for parallel execution
			t.Parallel()

			// Setup agent
			agent, ctx := SetupAgent(t)
			defer func() { _ = agent.Disconnect() }()

			// Create synchronization points
			handlerReady := testutil.NewSyncPoint()
			clientConnected := testutil.NewSyncPoint()
			requestComplete := testutil.NewSyncPoint()

			// Channel to pass client address information
			clientAddrChan := make(chan net.Addr, 1)

			// Create a server listener
			serverListener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err, "Failed to create server listener")
			defer serverListener.Close()

			// Extract server address to use for upstream
			serverAddr := serverListener.Addr().String()
			t.Logf("Local server running at: %s", serverAddr)

			// Start a goroutine to handle incoming connections on the local server
			go func() {
				// Signal that we're ready to accept connections
				handlerReady.Signal()

				// Accept a connection
				conn, err := serverListener.Accept()
				assert.NoError(t, err, "Failed to accept connection")
				if err != nil {
					return
				}
				defer conn.Close()
				t.Log("Connection accepted by local server")

				// Wait for client to connect before parsing PROXY header
				clientConnected.Wait(t)

				// Create a buffered reader for the connection
				reader := bufio.NewReader(conn)

				// Parse the PROXY protocol header
				srcAddr, dstAddr, err := parseProxyProtocolHeader(reader)
				assert.NoError(t, err, "Error parsing PROXY protocol header")
				if err != nil {
					return
				}

				// Log header details
				t.Logf("PROXY header parsed: src=%s, dst=%s", srcAddr, dstAddr)

				// Send the source address to the channel for verification
				clientAddrChan <- srcAddr

				// Handle connection based on endpoint type
				switch {
				case strings.HasPrefix(scheme, "tls"):
					handleTLSConnection(t, conn, reader, srcAddr)
				case strings.HasPrefix(scheme, "https"):
					handleHTTPConnection(t, conn, reader, srcAddr)
				default: // TCP
					handleTCPConnection(t, conn, reader, srcAddr)
				}

				// Signal that the request processing is complete
				requestComplete.Signal()
			}()

			// Wait for the handler to be ready to accept connections
			handlerReady.Wait(t)

			// Create a forwarder with PROXY protocol version 1 enabled
			// Format the upstream URL properly
			upstreamURL := fmt.Sprintf("tcp://%s", serverAddr)
			upstream := ngrok.WithUpstream(upstreamURL,
				ngrok.WithUpstreamProxyProto(ngrok.ProxyProtoV1), // Version 1 (text format)
			)
			forwarder, err := agent.Forward(ctx, upstream,
				ngrok.WithURL(scheme),
			)
			require.NoError(t, err, "Failed to create forwarder with PROXY protocol")
			defer forwarder.Close()

			// Verify the forwarder has PROXY protocol enabled
			proxyProto := forwarder.ProxyProtocol()
			require.Equal(t, ngrok.ProxyProtoV1, proxyProto, "ProxyProtocol should be ProxyProtoV1")
			t.Logf("Proxy protocol enabled: %s", proxyProto)

			// Log the endpoint URL
			endpointURL := forwarder.URL().String()
			t.Logf("Endpoint URL: %s", endpointURL)

			// Signal that the client is about to connect
			clientConnected.Signal()

			// Connect to the endpoint with appropriate client based on scheme
			switch {
			case strings.HasPrefix(scheme, "https"):
				connectHTTPSClient(ctx, t, endpointURL)
			case strings.HasPrefix(scheme, "tls"):
				connectTLSClient(t, endpointURL)
			default: // TCP
				connectTCPClient(ctx, t, endpointURL)
			}

			// Wait for the client address with timeout
			var clientAddr net.Addr
			select {
			case clientAddr = <-clientAddrChan:
				// Verify the client address
				verifyClientAddr(t, clientAddr)
			case <-time.After(2 * time.Second):
				require.Fail(t, "Timed out waiting for client address")
			}

			// Wait for request completion
			requestComplete.Wait(t)
		})
	}
}
