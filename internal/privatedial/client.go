// Package privatedial implements a client for the private-dial protocol
// served by ngrok's mux service. It authenticates with a tunnel authtoken
// and then multiplexes per-target net.Conn streams over a single HTTP/2
// connection.
//
// This package is vendored from the ngrok monorepo
// (go/lib/privatedial/client.go) at commit
// 5a243324af0d96cf6a7499c1a3a1483cdd4e5ef2. When pulling updates from the
// monorepo, replace errs/libmux/pb_private_dial imports as below and
// update the sync header in pb/private_dial.proto.
package privatedial

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/net/http2"

	pbpd "golang.ngrok.com/ngrok/v2/internal/privatedial/pb"
)

// ClientOpts configures a Client.
type ClientOpts struct {
	// ServerAddr is the "host:port" endpoint that serves the private-dial
	// protocol (e.g. "dial-endpoint.ngrok.com:443"). The net.Dial to this
	// address must reach a mux PrivateDialIngresses listener.
	ServerAddr string

	// ServerName is the SNI name used when negotiating TLS. Defaults to the
	// host portion of ServerAddr.
	ServerName string

	// AuthToken is a tunnel authtoken presented as "Authorization: Bearer".
	AuthToken string

	// TLSConfig overrides the default TLS config. MinVersion and NextProtos
	// are forced to TLS 1.3 / h2 respectively regardless.
	TLSConfig *tls.Config

	// ClientVersion is sent in the SessionReq.client_version field and
	// also as the User-Agent header. Optional.
	ClientVersion string

	// Metadata is sent in SessionReq.metadata for server-side logging.
	// Optional.
	Metadata map[string]string
}

// Client is a reusable factory for private-dial sessions.
type Client struct {
	opts ClientOpts
}

// NewClient returns a Client. It does not open any connection.
func NewClient(opts ClientOpts) *Client {
	if opts.ServerName == "" {
		if host, _, err := net.SplitHostPort(opts.ServerAddr); err == nil {
			opts.ServerName = host
		} else {
			opts.ServerName = opts.ServerAddr
		}
	}
	return &Client{opts: opts}
}

// OpenSession establishes a single HTTP/2 connection to the server, opens
// the control stream (/session), authenticates, and returns a Session. The
// caller must Close the Session to release the connection.
func (c *Client) OpenSession(ctx context.Context) (*Session, error) {
	tlsCfg := &tls.Config{}
	if c.opts.TLSConfig != nil {
		tlsCfg = c.opts.TLSConfig.Clone()
	}
	tlsCfg.ServerName = c.opts.ServerName
	tlsCfg.NextProtos = []string{"h2"}
	if tlsCfg.MinVersion == 0 {
		tlsCfg.MinVersion = tls.VersionTLS13
	}

	transport := &http2.Transport{
		TLSClientConfig: tlsCfg,
		AllowHTTP:       false,
	}

	controlURL := &url.URL{Scheme: "https", Host: c.opts.ServerAddr, Path: "/session"}
	dialURL := &url.URL{Scheme: "https", Host: c.opts.ServerAddr, Path: "/dial"}

	bodyReader, bodyWriter := io.Pipe()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, controlURL.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build /session request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.opts.AuthToken)
	req.Header.Set("Content-Type", "application/x-protobuf")
	if c.opts.ClientVersion != "" {
		req.Header.Set("User-Agent", c.opts.ClientVersion)
	}

	go func() {
		err := writeFrame(bodyWriter, &pbpd.SessionReq{
			ClientVersion: c.opts.ClientVersion,
			Metadata:      c.opts.Metadata,
		})
		if err != nil {
			_ = bodyWriter.CloseWithError(err)
		}
	}()

	resp, err := transport.RoundTrip(req)
	if err != nil {
		_ = bodyWriter.Close()
		transport.CloseIdleConnections()
		return nil, fmt.Errorf("private-dial /session: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		_ = bodyWriter.Close()
		transport.CloseIdleConnections()
		return nil, fmt.Errorf("private-dial /session status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	ack := new(pbpd.SessionAck)
	if err := readFrame(resp.Body, ack); err != nil {
		_ = resp.Body.Close()
		_ = bodyWriter.Close()
		transport.CloseIdleConnections()
		return nil, fmt.Errorf("read SessionAck: %w", err)
	}

	sess := &Session{
		serverID:       ack.GetServerId(),
		pingInterval:   ack.GetPingInterval().AsDuration(),
		transport:      transport,
		dialURL:        dialURL,
		authToken:      c.opts.AuthToken,
		sessionReq:     &pbpd.SessionReq{ClientVersion: c.opts.ClientVersion, Metadata: c.opts.Metadata},
		controlResp:    resp,
		controlReqBody: bodyWriter,
		pings:          map[uint64]time.Time{},
		drainCh:        make(chan struct{}),
		serverErrCh:    make(chan error, 1),
		stopCh:         make(chan struct{}),
	}
	sess.responseReceived.Store(true)
	go sess.readControl()
	if sess.pingInterval > 0 {
		go sess.pingLoop()
	}

	return sess, nil
}

// Session represents an authenticated private-dial session. It multiplexes
// Dial calls over a single HTTP/2 connection.
type Session struct {
	serverID     string
	pingInterval time.Duration

	transport *http2.Transport
	dialURL   *url.URL
	authToken string

	sessionReq       *pbpd.SessionReq
	responseReceived atomic.Bool

	controlResp    *http.Response
	controlReqBody *io.PipeWriter

	writeMu sync.Mutex

	pingsMu sync.Mutex
	pings   map[uint64]time.Time

	drainOnce   sync.Once
	drainCh     chan struct{}
	serverErrCh chan error

	stopCh chan struct{}

	rttMu   sync.Mutex
	lastRTT time.Duration

	closeOnce sync.Once
	closeErr  error
}

// ServerID returns the opaque identifier the server emitted in SessionAck.
// Useful for log correlation across reconnects.
func (s *Session) ServerID() string { return s.serverID }

// PingInterval is the cadence at which the server expects to send and
// receive Ping frames on the control stream.
func (s *Session) PingInterval() time.Duration { return s.pingInterval }

// LastRTT returns the most recent round-trip time measured by the
// client-side ping loop. Zero before the first pong arrives.
func (s *Session) LastRTT() time.Duration {
	s.rttMu.Lock()
	defer s.rttMu.Unlock()
	return s.lastRTT
}

// DrainCh is closed when the server sends a PleaseDrain control frame.
// Callers should stop issuing new Dial calls once this fires.
func (s *Session) DrainCh() <-chan struct{} { return s.drainCh }

// ServerErrCh delivers, at most once, an error from the control stream (I/O
// failure, explicit SessionError frame, or clean EOF as io.EOF). After a
// value is delivered the session is effectively dead.
func (s *Session) ServerErrCh() <-chan error { return s.serverErrCh }

// Dial opens a new stream targeting host:port within the caller's account.
// The returned net.Conn is a bidirectional byte stream; closing it releases
// only that stream, not the session.
//
// Server-side dial failures are translated to standard net errors so
// callers can use errors.Is/As against the usual sentinels:
//   - "endpoint not found" returns a *net.OpError wrapping
//     *net.DNSError (with IsNotFound: true) — analogous to a DNS
//     lookup miss.
//   - "no endpoint available" / "session draining" returns a
//     *net.OpError wrapping syscall.ECONNREFUSED — analogous to a
//     refused TCP connect.
//   - other non-200 responses return a generic *net.OpError with the
//     server's error body as the wrapped Err.
func (s *Session) Dial(ctx context.Context, host string, port int) (net.Conn, error) {
	addr := dialAddr{host: host, port: port}

	reqReader, reqWriter := io.Pipe()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.dialURL.String(), reqReader)
	if err != nil {
		_ = reqWriter.Close()
		return nil, &net.OpError{Op: "dial", Net: addr.Network(), Addr: addr, Err: fmt.Errorf("build /dial request: %w", err)}
	}
	req.Header.Set("Authorization", "Bearer "+s.authToken)
	req.Header.Set("Content-Type", "application/octet-stream")

	dreq := &pbpd.DialReq{
		Host: host,
		Port: int64(port),
	}
	if !s.responseReceived.Load() {
		dreq.SessionReq = s.sessionReq
	}

	go func() {
		err := writeFrame(reqWriter, dreq)
		if err != nil {
			_ = reqWriter.CloseWithError(err)
		}
	}()

	resp, err := s.transport.RoundTrip(req)
	if err != nil {
		_ = reqWriter.Close()
		return nil, &net.OpError{Op: "dial", Net: addr.Network(), Addr: addr, Err: err}
	}
	s.responseReceived.Store(true)
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		_ = reqWriter.Close()
		return nil, dialResponseError(resp, addr, string(snippet))
	}

	return &dialConn{
		reqBody:  reqWriter,
		respBody: resp.Body,
	}, nil
}

const dialErrorCodeHeader = "Ngrok-Error-Code"

// dialResponseError translates a non-200 /dial response into a
// *net.OpError that wraps a *ServerError carrying the structured ngrok
// error code (when the server sent one). The ServerError's Unwrap
// chain points at a familiar net sentinel — *net.DNSError for "not
// found" / 404, syscall.ECONNREFUSED for "no endpoint" / 503, etc. —
// so callers can use either errors.As(&serverErr) for the ngrok code
// or errors.Is(err, syscall.ECONNREFUSED) / errors.As(&dnsErr) for
// generic "couldn't connect" handling.
func dialResponseError(resp *http.Response, addr dialAddr, body string) error {
	body = strings.TrimSpace(body)
	code := resp.Header.Get(dialErrorCodeHeader)

	var sentinel error
	switch resp.StatusCode {
	case http.StatusNotFound:
		sentinel = &net.DNSError{Err: "no such host", Name: addr.host, IsNotFound: true}
	case http.StatusServiceUnavailable:
		sentinel = &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
		sentinel = &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}
	}

	server := &ServerError{
		Code:    code,
		Message: body,
		Status:  resp.StatusCode,
		wrapped: sentinel,
	}
	return &net.OpError{Op: "dial", Net: addr.Network(), Addr: addr, Err: server}
}

// ServerError carries the structured error a private-dial server returned
// on a non-200 /dial response. It is wrapped inside *net.OpError so
// callers who only care about the broad failure category can use
// errors.Is / errors.As against the standard net sentinels (e.g.
// *net.DNSError, syscall.ECONNREFUSED). Callers that want the ngrok
// error code or the server's human-readable message can extract it via
// errors.As(err, &serverErr).
type ServerError struct {
	// Code is the prefixed ngrok error code (e.g. "ERR_NGROK_706")
	// extracted from the Ngrok-Error-Code response header. May be
	// empty if the server did not emit one.
	Code string

	// Message is the server-provided human-readable description.
	Message string

	// Status is the HTTP status code of the response.
	Status int

	// wrapped is the net-style sentinel (*net.DNSError,
	// *os.SyscallError{ECONNREFUSED}, etc.) used for errors.Is /
	// errors.As composition.
	wrapped error
}

func (e *ServerError) Error() string {
	switch {
	case e.Code != "" && e.Message != "":
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	case e.Code != "":
		return e.Code
	case e.Message != "":
		return e.Message
	default:
		return fmt.Sprintf("private-dial server returned %d", e.Status)
	}
}

// Unwrap returns the standard-library sentinel error chosen for this
// ServerError's status code, so errors.Is / errors.As walks down to it.
func (e *ServerError) Unwrap() error { return e.wrapped }

// Close tears down the control stream and the underlying HTTP/2 connection.
// Any in-flight Dial streams will see read/write errors.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		close(s.stopCh)
		_ = s.controlReqBody.Close()
		if s.controlResp != nil {
			_ = s.controlResp.Body.Close()
		}
		s.transport.CloseIdleConnections()
	})
	return s.closeErr
}

// sendControlFrame serializes writes to the control request body. Both
// pingLoop (Ping) and readControl (Pong echoes) push frames here.
func (s *Session) sendControlFrame(frame *pbpd.ControlFrame) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return writeFrame(s.controlReqBody, frame)
}

// pingLoop sends a Ping every pingInterval and records RTT when the
// matching Pong arrives. It stops on Close.
func (s *Session) pingLoop() {
	tick := time.NewTicker(s.pingInterval)
	defer tick.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-tick.C:
			token := s.recordPingSent(time.Now())
			if err := s.sendControlFrame(&pbpd.ControlFrame{
				Frame: &pbpd.ControlFrame_Ping{Ping: &pbpd.Ping{Token: token}},
			}); err != nil {
				return
			}
		}
	}
}

func (s *Session) recordPingSent(now time.Time) uint64 {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	token := binary.LittleEndian.Uint64(buf[:])
	s.pingsMu.Lock()
	s.pings[token] = now
	s.pingsMu.Unlock()
	return token
}

func (s *Session) completePing(token uint64, now time.Time) (time.Duration, bool) {
	s.pingsMu.Lock()
	defer s.pingsMu.Unlock()
	sent, ok := s.pings[token]
	if !ok {
		return 0, false
	}
	delete(s.pings, token)
	return now.Sub(sent), true
}

// readControl pumps ControlFrames from the server until EOF or error.
// It closes drainCh on PleaseDrain, echoes Pong on inbound Ping (so the
// server can measure its RTT to us), records client-side RTT on Pong,
// and forwards terminal errors to serverErrCh.
func (s *Session) readControl() {
	defer func() {
		select {
		case s.serverErrCh <- io.EOF:
		default:
		}
	}()
	r := bufio.NewReader(s.controlResp.Body)
	for {
		frame := new(pbpd.ControlFrame)
		if err := readFrame(r, frame); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return
			}
			select {
			case s.serverErrCh <- err:
			default:
			}
			return
		}
		switch f := frame.Frame.(type) {
		case *pbpd.ControlFrame_PleaseDrain:
			s.drainOnce.Do(func() { close(s.drainCh) })
		case *pbpd.ControlFrame_SessionError:
			select {
			case s.serverErrCh <- fmt.Errorf("server session error: %s", f.SessionError.GetMessage()):
			default:
			}
			return
		case *pbpd.ControlFrame_Ping:
			_ = s.sendControlFrame(&pbpd.ControlFrame{
				Frame: &pbpd.ControlFrame_Pong{Pong: &pbpd.Pong{Token: f.Ping.GetToken()}},
			})
		case *pbpd.ControlFrame_Pong:
			if rtt, ok := s.completePing(f.Pong.GetToken(), time.Now()); ok {
				s.rttMu.Lock()
				s.lastRTT = rtt
				s.rttMu.Unlock()
			}
		}
	}
}

// dialConn is the net.Conn returned from Session.Dial. Read pulls from the
// HTTP/2 response body; Write pushes into the HTTP/2 request body. Close
// terminates both sides. Dial-level failures never reach a dialConn —
// they're returned from Dial directly as *net.OpError. EOF on Read
// surfaces normally as io.EOF.
type dialConn struct {
	reqBody  *io.PipeWriter
	respBody io.ReadCloser

	closeOnce sync.Once
}

func (c *dialConn) Read(p []byte) (int, error)  { return c.respBody.Read(p) }
func (c *dialConn) Write(p []byte) (int, error) { return c.reqBody.Write(p) }

func (c *dialConn) Close() error {
	c.closeOnce.Do(func() {
		_ = c.reqBody.Close()
		_ = c.respBody.Close()
	})
	return nil
}

func (c *dialConn) LocalAddr() net.Addr                { return dialAddr{} }
func (c *dialConn) RemoteAddr() net.Addr               { return dialAddr{} }
func (c *dialConn) SetDeadline(_ time.Time) error      { return nil }
func (c *dialConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *dialConn) SetWriteDeadline(_ time.Time) error { return nil }

// dialAddr is the net.Addr we hand back to callers and use in net.OpError
// targets. Network is always "privatedial"; String renders as "host:port"
// when populated, mirroring what callers expect from a dial target.
type dialAddr struct {
	host string
	port int
}

func (a dialAddr) Network() string { return "privatedial" }
func (a dialAddr) String() string {
	if a.host == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", a.host, a.port)
}
