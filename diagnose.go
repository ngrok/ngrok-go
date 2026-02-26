package ngrok

import (
	"cmp"
	"context"
	"crypto/tls"
	"errors"
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

// DiagnoseResult holds the outcome of a successful diagnostic probe.
type DiagnoseResult struct {
	// The address that was tested (ip:port or host:port).
	Addr string
	// Region reported by SrvInfo.
	Region string
	// Round-trip latency of the SrvInfo call.
	Latency time.Duration
}

// diagnoseError is returned by [Diagnoser.Diagnose] when a probe step fails.
// Use [IsTCPDiagnoseFailure], [IsTLSDiagnoseFailure], or
// [IsMuxadoDiagnoseFailure] to determine which step failed.
type diagnoseError struct {
	// Step is the probe step that failed: "tcp", "tls", or "muxado".
	Step string
	// Err is the underlying error.
	Err error
}

func (e *diagnoseError) Error() string {
	return fmt.Sprintf("diagnose %s: %v", e.Step, e.Err)
}

func (e *diagnoseError) Unwrap() error { return e.Err }

// IsTCPDiagnoseFailure reports whether err is a TCP-level probe failure.
func IsTCPDiagnoseFailure(err error) bool {
	var de *diagnoseError
	return errors.As(err, &de) && de.Step == "tcp"
}

// IsTLSDiagnoseFailure reports whether err is a TLS-level probe failure.
func IsTLSDiagnoseFailure(err error) bool {
	var de *diagnoseError
	return errors.As(err, &de) && de.Step == "tls"
}

// IsMuxadoDiagnoseFailure reports whether err is a muxado-level probe failure.
func IsMuxadoDiagnoseFailure(err error) bool {
	var de *diagnoseError
	return errors.As(err, &de) && de.Step == "muxado"
}

// Diagnoser is implemented by Agent types that support pre-connection
// diagnostic probing. Use a type assertion to access it:
//
//	d, ok := agent.(ngrok.Diagnoser)
type Diagnoser interface {
	Agent

	// Diagnose tests connectivity to addr by probing TCP, TLS, and the Muxado
	// tunnel protocol. It uses the Agent's configured TLS settings, CA roots,
	// and proxy/dialer settings.
	//
	// If addr is empty, the configured server address is probed.
	//
	// This method does NOT establish a persistent session or call Auth. It is
	// safe to call without affecting any existing connection.
	Diagnose(ctx context.Context, addr string) (DiagnoseResult, error)
}

// Diagnose implements Diagnoser.
func (a *agent) Diagnose(ctx context.Context, addr string) (DiagnoseResult, error) {
	connectAddr := cmp.Or(a.opts.connectURL, "connect.ngrok-agent.com:443")
	if addr == "" {
		addr = connectAddr
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
		return DiagnoseResult{}, err
	}

	logger := cmp.Or(a.opts.logger, slog.Default())

	return a.probeAddr(ctx, logger, dialer, serverName, addr)
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

// probeAddr runs TCP → TLS → Muxado → SrvInfo for addr and returns a
// DiagnoseResult on success, or a *DiagnoseError indicating which step failed.
func (a *agent) probeAddr(ctx context.Context, logger *slog.Logger, dialer Dialer, serverName, addr string) (DiagnoseResult, error) {
	result := DiagnoseResult{Addr: addr}

	// TCP
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return result, &diagnoseError{Step: "tcp", Err: err}
	}
	defer conn.Close() //nolint:errcheck

	// Interrupt I/O if the context is cancelled or expires.
	stop := context.AfterFunc(ctx, func() {
		conn.SetDeadline(time.Now()) //nolint:errcheck
	})
	defer stop()

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
		return result, &diagnoseError{Step: "tls", Err: err}
	}

	// Muxado + SrvInfo
	muxSess := muxado.Client(tlsConn, nil)
	raw := tunnelclient.NewRawSession(logger, muxSess, nil, nopSessionHandler{})
	defer raw.Close() //nolint:errcheck

	start := time.Now()
	info, err := raw.SrvInfo()
	if err != nil {
		return result, &diagnoseError{Step: "muxado", Err: err}
	}
	result.Region = info.Region
	result.Latency = time.Since(start)
	return result, nil
}

// nopSessionHandler is a minimal SessionHandler that ignores all server RPCs.
// It is used by probeAddr, which never calls Accept() and therefore will never
// dispatch to these methods.
type nopSessionHandler struct{}

func (nopSessionHandler) OnStop(*proto.Stop, tunnelclient.HandlerRespFunc)             {}
func (nopSessionHandler) OnRestart(*proto.Restart, tunnelclient.HandlerRespFunc)       {}
func (nopSessionHandler) OnUpdate(*proto.Update, tunnelclient.HandlerRespFunc)         {}
func (nopSessionHandler) OnStopTunnel(*proto.StopTunnel, tunnelclient.HandlerRespFunc) {}
