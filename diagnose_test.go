package ngrok

import (
	"crypto/tls"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	muxado "golang.ngrok.com/muxado/v2"

	"golang.ngrok.com/ngrok/v2/internal/testcontext"
	"golang.ngrok.com/ngrok/v2/internal/tlstest"
	"golang.ngrok.com/ngrok/v2/internal/tunnel/proto"
)

// TestDiagnoseTCPFailure verifies that a connection refused at the TCP level
// is reported as a TCP step failure.
func TestDiagnoseTCPFailure(t *testing.T) {
	// Bind and immediately close a listener so the port is unreachable.
	l, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	_ = l.Close()

	a, err := NewAgent()
	require.NoError(t, err)

	result, err := a.Diagnose(testcontext.ForTB(t), addr)
	require.Error(t, err)
	assert.True(t, IsTCPDiagnoseFailure(err))
	assert.Equal(t, addr, result.Addr)
}

// TestDiagnoseTLSFailure verifies that a TCP-only server (no TLS) is reported
// as a TLS step failure.
func TestDiagnoseTLSFailure(t *testing.T) {
	l, err := net.Listen("tcp", "localhost:0")
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

	result, err := a.Diagnose(testcontext.ForTB(t), l.Addr().String())
	require.Error(t, err)
	assert.True(t, IsTLSDiagnoseFailure(err))
	assert.Equal(t, l.Addr().String(), result.Addr)
}

// TestDiagnoseMuxadoSuccess verifies the full happy path: TCP → TLS → Muxado
// → SrvInfo all succeed against a local test server.
func TestDiagnoseMuxadoSuccess(t *testing.T) {
	cert, err := tlstest.CreateCertificate()
	if err != nil {
		t.Fatal(err)
	}
	tlsServerCfg := &tls.Config{
		Certificates: []tls.Certificate{*cert},
	}

	l, err := tls.Listen("tcp", "localhost:0", tlsServerCfg)
	require.NoError(t, err)
	defer l.Close() //nolint:errcheck

	const testRegion = "test-us"

	// Run a minimal Muxado server that responds to a single SrvInfo RPC.
	muxadoDone := make(chan struct{})
	defer func() { <-muxadoDone }()
	go func() {
		defer close(muxadoDone)
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
				assert.NoError(t, json.NewEncoder(stream).Encode(proto.SrvInfoResp{Region: testRegion}))
				assert.NoError(t, stream.Close())
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

	result, err := a.Diagnose(testcontext.ForTB(t), l.Addr().String())
	require.NoError(t, err)
	assert.Equal(t, l.Addr().String(), result.Addr)
	assert.Equal(t, testRegion, result.Region)
	assert.Greater(t, result.Latency, time.Duration(0))
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

	ctx := testcontext.ForTB(t)

	result, err := a.Diagnose(ctx, serverAddr)
	require.NoError(t, err)
	t.Logf("addr=%s region=%s latency=%s", result.Addr, result.Region, result.Latency)
	assert.NotEmpty(t, result.Region)
	assert.Greater(t, result.Latency, time.Duration(0))
}
