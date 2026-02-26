package ngrok

import (
	"cmp"
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"time"

	"golang.org/x/net/proxy"

	muxado "golang.ngrok.com/muxado/v2"

	"golang.ngrok.com/ngrok/v2/internal/legacy"
	tunnelclient "golang.ngrok.com/ngrok/v2/internal/tunnel/client"
	"golang.ngrok.com/ngrok/v2/internal/tunnel/proto"
)

// DiagnoseResult holds the outcome of a single diagnostic probe against one
// tunnel server address.
type DiagnoseResult struct {
	// The address that was tested (ip:port or host:port).
	Addr string
	// Which steps succeeded, in order attempted: "tcp", "tls", "muxado".
	CompletedSteps []string
	// Region reported by SrvInfo (empty if the muxado step did not complete).
	Region string
	// Round-trip latency of the SrvInfo call (zero if the muxado step did not
	// complete).
	Latency time.Duration
	// First error encountered, nil if all steps passed.
	Err error
	// The step that failed: "tcp", "tls", or "muxado". Empty if all passed.
	FailedStep string
}

// Diagnoser is implemented by Agent types that support pre-connection
// diagnostic probing. Use a type assertion to access it:
//
//	d, ok := agent.(ngrok.Diagnoser)
type Diagnoser interface {
	Agent

	// Diagnose tests connectivity to the configured tunnel server by probing
	// each address in addrs independently through TCP, TLS, and the Muxado
	// tunnel protocol. It uses the Agent's configured TLS settings, CA roots,
	// and proxy/dialer settings.
	//
	// If addrs is nil or empty, the configured server address is probed as-is.
	//
	// This method does NOT establish a persistent session or call Auth. It is
	// safe to call without affecting any existing connection.
	Diagnose(ctx context.Context, addrs []string) ([]DiagnoseResult, error)
}

// Diagnose implements Diagnosable.
func (a *agent) Diagnose(ctx context.Context, addrs []string) ([]DiagnoseResult, error) {
	connectAddr := a.opts.connectURL
	if connectAddr == "" {
		connectAddr = "connect.ngrok-agent.com:443"
	}
	if len(addrs) == 0 {
		addrs = []string{connectAddr}
	}

	// Derive the TLS ServerName from the configured connect hostname, not from
	// the addr under test (which may be an IP address that cannot be used for
	// SNI).
	serverName, _, err := net.SplitHostPort(connectAddr)
	if err != nil {
		// connectAddr has no port — use as-is.
		serverName = connectAddr
	}

	dialer, err := a.buildDiagnosticDialer()
	if err != nil {
		return nil, err
	}

	logger := cmp.Or(a.opts.logger, slog.Default())

	results := make([]DiagnoseResult, 0, len(addrs))
	for _, addr := range addrs {
		results = append(results, a.probeAddr(ctx, logger, dialer, serverName, addr))
	}
	return results, nil
}

// buildDiagnosticDialer returns the effective dialer for probes, applying
// proxy configuration without mutating agent state.
func (a *agent) buildDiagnosticDialer() (Dialer, error) {
	baseDialer := cmp.Or(a.opts.dialer, Dialer(&net.Dialer{}))
	if a.opts.proxyURL == "" {
		return baseDialer, nil
	}
	parsedURL, err := url.Parse(a.opts.proxyURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}
	proxyDialer, err := proxy.FromURL(parsedURL, baseDialer)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize proxy: %w", err)
	}
	dialer, ok := proxyDialer.(Dialer)
	if !ok {
		return nil, fmt.Errorf("proxy dialer is not compatible with ngrok Dialer interface")
	}
	return dialer, nil
}

// probeAddr runs TCP → TLS → Muxado → SrvInfo for a single address and
// returns a DiagnoseResult recording which steps passed.
func (a *agent) probeAddr(ctx context.Context, logger *slog.Logger, dialer Dialer, serverName, addr string) DiagnoseResult {
	result := DiagnoseResult{Addr: addr}

	// TCP
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		result.Err = err
		result.FailedStep = "tcp"
		return result
	}
	defer conn.Close() //nolint:errcheck

	// Interrupt I/O if the context is cancelled or expires.
	stop := context.AfterFunc(ctx, func() {
		conn.SetDeadline(time.Now()) //nolint:errcheck
	})
	defer stop()

	result.CompletedSteps = append(result.CompletedSteps, "tcp")

	// TLS
	rootCAs := a.opts.connectCAs
	if rootCAs == nil {
		rootCAs = legacy.DefaultCAPool()
	}
	tlsCfg := &tls.Config{
		RootCAs:    rootCAs,
		ServerName: serverName,
		MinVersion: tls.VersionTLS12,
	}
	if a.opts.tlsConfig != nil {
		a.opts.tlsConfig(tlsCfg)
	}
	tlsConn := tls.Client(conn, tlsCfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		result.Err = err
		result.FailedStep = "tls"
		return result
	}
	result.CompletedSteps = append(result.CompletedSteps, "tls")

	// Muxado + SrvInfo
	muxSess := muxado.Client(tlsConn, nil)
	raw := tunnelclient.NewRawSession(logger, muxSess, nil, nopSessionHandler{})
	defer raw.Close() //nolint:errcheck

	start := time.Now()
	info, err := raw.SrvInfo()
	if err != nil {
		result.Err = err
		result.FailedStep = "muxado"
		return result
	}
	result.CompletedSteps = append(result.CompletedSteps, "muxado")
	result.Region = info.Region
	result.Latency = time.Since(start)
	return result
}

// nopSessionHandler is a minimal SessionHandler that ignores all server RPCs.
// It is used by probeAddr, which never calls Accept() and therefore will never
// dispatch to these methods.
type nopSessionHandler struct{}

func (nopSessionHandler) OnStop(*proto.Stop, tunnelclient.HandlerRespFunc)             {}
func (nopSessionHandler) OnRestart(*proto.Restart, tunnelclient.HandlerRespFunc)       {}
func (nopSessionHandler) OnUpdate(*proto.Update, tunnelclient.HandlerRespFunc)         {}
func (nopSessionHandler) OnStopTunnel(*proto.StopTunnel, tunnelclient.HandlerRespFunc) {}
