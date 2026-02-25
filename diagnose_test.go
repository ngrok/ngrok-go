package ngrok

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	muxado "golang.ngrok.com/muxado/v2"

	"golang.ngrok.com/ngrok/v2/internal/tunnel/proto"
)

// TestDiagnoseTCPFailure verifies that a connection refused at the TCP level
// is reported as a TCP step failure.
func TestDiagnoseTCPFailure(t *testing.T) {
	// Bind and immediately close a listener so the port is unreachable.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	_ = l.Close()

	a, err := NewAgent()
	require.NoError(t, err)

	d, ok := a.(Diagnosable)
	require.True(t, ok, "agent should implement Diagnosable")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results, err := d.Diagnose(ctx, []string{addr})
	require.NoError(t, err)
	require.Len(t, results, 1)

	r := results[0]
	assert.Equal(t, addr, r.Addr)
	assert.Equal(t, "tcp", r.FailedStep)
	assert.NotNil(t, r.Err)
	assert.Empty(t, r.CompletedSteps)
}

// TestDiagnoseTLSFailure verifies that a TCP-only server (no TLS) is reported
// as a TLS step failure with TCP marked as succeeded.
func TestDiagnoseTLSFailure(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close() //nolint:errcheck

	// Accept one connection and immediately close it.
	go func() {
		conn, err := l.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	a, err := NewAgent(WithAgentConnectURL(l.Addr().String()))
	require.NoError(t, err)

	d := a.(Diagnosable)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results, err := d.Diagnose(ctx, []string{l.Addr().String()})
	require.NoError(t, err)
	require.Len(t, results, 1)

	r := results[0]
	assert.Equal(t, []string{"tcp"}, r.CompletedSteps)
	assert.Equal(t, "tls", r.FailedStep)
	assert.NotNil(t, r.Err)
}

// TestDiagnoseMuxadoSuccess verifies the full happy path: TCP → TLS → Muxado
// → SrvInfo all succeed against a local test server.
func TestDiagnoseMuxadoSuccess(t *testing.T) {
	// Generate a self-signed TLS certificate for the test server.
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "diagnose-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	require.NoError(t, err)

	tlsServerCfg := &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{certDER}, PrivateKey: priv}},
	}

	l, err := tls.Listen("tcp", "127.0.0.1:0", tlsServerCfg)
	require.NoError(t, err)
	defer l.Close() //nolint:errcheck

	const testRegion = "test-us"

	// Run a minimal Muxado server that responds to a single SrvInfo RPC.
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck

		typed := muxado.NewTypedStreamSession(muxado.Server(conn, nil))
		for {
			stream, err := typed.AcceptTypedStream()
			if err != nil {
				return
			}
			streamType := proto.ReqType(stream.StreamType())
			if streamType == proto.SrvInfoReq {
				var req proto.SrvInfo
				_ = json.NewDecoder(stream).Decode(&req)
				_ = json.NewEncoder(stream).Encode(proto.SrvInfoResp{Region: testRegion})
				_ = stream.Close()
				return
			}
			// Drain any other stream types (e.g. heartbeat).
			_ = stream.Close()
		}
	}()

	a, err := NewAgent(
		WithAgentConnectURL(l.Addr().String()),
		WithTLSConfig(func(c *tls.Config) { c.InsecureSkipVerify = true }),
	)
	require.NoError(t, err)

	d := a.(Diagnosable)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results, err := d.Diagnose(ctx, []string{l.Addr().String()})
	require.NoError(t, err)
	require.Len(t, results, 1)

	r := results[0]
	assert.Empty(t, r.FailedStep)
	assert.Nil(t, r.Err)
	assert.Equal(t, []string{"tcp", "tls", "muxado"}, r.CompletedSteps)
	assert.Equal(t, testRegion, r.Region)
	assert.Greater(t, r.Latency, time.Duration(0))
}

// TestDiagnoseMultipleAddrs verifies that Diagnose probes each address
// independently and returns a result per address.
func TestDiagnoseMultipleAddrs(t *testing.T) {
	// One reachable TCP listener (TLS failure is expected — no TLS server).
	lTCP, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer lTCP.Close() //nolint:errcheck

	go func() {
		for {
			conn, err := lTCP.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	// One closed port (TCP failure expected).
	lClosed, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closedAddr := lClosed.Addr().String()
	_ = lClosed.Close()

	a, err := NewAgent(WithAgentConnectURL(lTCP.Addr().String()))
	require.NoError(t, err)

	d := a.(Diagnosable)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	addrs := []string{lTCP.Addr().String(), closedAddr}
	results, err := d.Diagnose(ctx, addrs)
	require.NoError(t, err)
	require.Len(t, results, 2)

	assert.Equal(t, lTCP.Addr().String(), results[0].Addr)
	assert.Equal(t, "tls", results[0].FailedStep)

	assert.Equal(t, closedAddr, results[1].Addr)
	assert.Equal(t, "tcp", results[1].FailedStep)
}

// TestDiagnoseOnline connects to a live tunnel server and verifies the full
// probe succeeds. Requires NGROK_TEST_ONLINE=1 or NGROK_TEST_ALL=1.
func TestDiagnoseOnline(t *testing.T) {
	if os.Getenv("NGROK_TEST_ONLINE") == "" && os.Getenv("NGROK_TEST_ALL") == "" {
		t.Skip("skipping online test; set NGROK_TEST_ONLINE=1 to run")
	}

	serverAddr := os.Getenv("NGROK_CONNECT_URL")
	if serverAddr == "" {
		serverAddr = "connect.ngrok-agent.com:443"
	}

	agentOpts := []AgentOption{WithAgentConnectURL(serverAddr)}
	if os.Getenv("NGROK_TEST_INSECURE") != "" {
		agentOpts = append(agentOpts, WithTLSConfig(func(c *tls.Config) {
			c.InsecureSkipVerify = true
		}))
	}

	a, err := NewAgent(agentOpts...)
	require.NoError(t, err)

	d, ok := a.(Diagnosable)
	require.True(t, ok)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results, err := d.Diagnose(ctx, []string{serverAddr})
	require.NoError(t, err)
	require.Len(t, results, 1)

	r := results[0]
	t.Logf("addr=%s steps=%v region=%s latency=%s err=%v",
		r.Addr, r.CompletedSteps, r.Region, r.Latency, r.Err)

	assert.Empty(t, r.FailedStep)
	assert.Nil(t, r.Err)
	assert.Equal(t, []string{"tcp", "tls", "muxado"}, r.CompletedSteps)
	assert.NotEmpty(t, r.Region)
	assert.Greater(t, r.Latency, time.Duration(0))
}
