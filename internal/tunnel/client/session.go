package client

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/ngrok/libngrok-go/internal/tunnel/netx"
	"github.com/ngrok/libngrok-go/internal/tunnel/proto"

	log "github.com/inconshreveable/log15"
	muxado "github.com/ngrok/libngrok-go/internal/muxado"
)

// Session is a higher-level client session interface. You will almost always prefer this over
// RawSession.
//
// Unlike RawSession, when you call Listen on a Session, you are returned
// a Tunnel object which can be used to accept new connections or close the
// remotely-bound listener.
//
// Session doesn't expose an Unlisten method, but rather expects you to call
// Close() on a returned Tunnel object. It also provides a convenience method
// for each protocol you can multiplex with.
type Session interface {
	// Auth begins a session with the remote server. You *must* call this before
	// calling any of the Listen functions.
	Auth(extra proto.AuthExtra) (proto.AuthResp, error)

	// Listen negotiates with the server to create a new remote listen for the
	// given protocol and options. It returns a *Tunnel on success from which
	// the caller can accept new connections over the listen.
	//
	// Applications will typically prefer to call the protocol-specific methods like
	// ListenHTTP, ListenTCP, etc.
	Listen(protocol string, opts any, extra proto.BindExtra, forwardsTo string) (Tunnel, error)

	// Listen negotiates with the server to create a new remote listen for the
	// given labels. It returns a *Tunnel on success from which the caller can
	// accept new connections over the listen.
	ListenLabel(labels map[string]string, metadata string, forwardsTo string) (Tunnel, error)

	// Convenience methods

	// ListenHTTP listens on a new HTTP endpoint
	ListenHTTP(opts *proto.HTTPOptions, extra proto.BindExtra, forwardsTo string) (Tunnel, error)

	// ListenHTTP listens on a new HTTPS endpoint
	ListenHTTPS(opts *proto.HTTPOptions, extra proto.BindExtra, forwardsTo string) (Tunnel, error)

	// ListenTCP listens on a remote TCP endpoint
	ListenTCP(opts *proto.TCPOptions, extra proto.BindExtra, forwardsTo string) (Tunnel, error)

	// ListenTLS listens on a remote TLS endpoint
	ListenTLS(opts *proto.TLSOptions, extra proto.BindExtra, forwardsTo string) (Tunnel, error)

	SrvInfo() (proto.SrvInfoResp, error)

	// Send a muxado heartbeat and record the latency
	Heartbeat() (time.Duration, error)

	// Latency updates
	Latency() <-chan time.Duration

	// Closes the session
	Close() error
}

type session struct {
	raw RawSession
	sync.RWMutex
	log.Logger
	tunnels map[string]*tunnel
}

// NewSession starts a new go-tunnel client session running over the given
// muxado session.
func NewSession(logger log.Logger, mux muxado.Session, heartbeatConfig *muxado.HeartbeatConfig, handler SessionHandler) Session {
	logger = newLogger(logger)
	s := &session{
		raw:     newRawSession(mux, logger, heartbeatConfig, handler),
		Logger:  logger,
		tunnels: make(map[string]*tunnel),
	}

	go s.receive()
	return s
}

func (s *session) Auth(extra proto.AuthExtra) (resp proto.AuthResp, err error) {
	resp, err = s.raw.Auth("", extra)
	if err != nil {
		return
	}
	if resp.Error != "" {
		err = errors.New(resp.Error)
		return
	}
	return
}

func (s *session) Latency() <-chan time.Duration {
	return s.raw.Latency()
}

func (s *session) Heartbeat() (time.Duration, error) {
	return s.raw.Heartbeat()
}

func (s *session) Listen(protocol string, opts any, extra proto.BindExtra, forwardsTo string) (Tunnel, error) {
	resp, err := s.raw.Listen(protocol, opts, extra, "", forwardsTo)
	if err != nil {
		return nil, err
	}

	// process application-level error
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	// make tunnel
	t := newTunnel(resp, extra, s, forwardsTo)

	// add to tunnel registry
	s.addTunnel(resp.ClientID, t)

	return t, nil
}

func (s *session) ListenLabel(labels map[string]string, metadata string, forwardsTo string) (Tunnel, error) {
	resp, err := s.raw.ListenLabel(labels, metadata, forwardsTo)
	if err != nil {
		return nil, err
	}

	// process application-level error
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	// make tunnel
	t := newTunnelLabel(resp, metadata, labels, s, forwardsTo)

	// add to tunnel registry
	s.addTunnel(resp.ID, t)

	return t, nil
}

func (s *session) ListenHTTP(opts *proto.HTTPOptions, extra proto.BindExtra, forwardsTo string) (Tunnel, error) {
	return s.Listen("http", opts, extra, forwardsTo)
}

func (s *session) ListenHTTPS(opts *proto.HTTPOptions, extra proto.BindExtra, forwardsTo string) (Tunnel, error) {
	return s.Listen("https", opts, extra, forwardsTo)
}

func (s *session) ListenTCP(opts *proto.TCPOptions, extra proto.BindExtra, forwardsTo string) (Tunnel, error) {
	return s.Listen("tcp", opts, extra, forwardsTo)
}

func (s *session) ListenTLS(opts *proto.TLSOptions, extra proto.BindExtra, forwardsTo string) (Tunnel, error) {
	return s.Listen("tls", opts, extra, forwardsTo)
}

func (s *session) ListenSSH(opts *proto.SSHOptions, extra proto.BindExtra, forwardsTo string) (Tunnel, error) {
	return s.Listen("ssh", opts, extra, forwardsTo)
}

func (s *session) SrvInfo() (proto.SrvInfoResp, error) {
	return s.raw.SrvInfo()
}

func (s *session) Close() error {
	return s.raw.Close()
}

func (s *session) receive() {
	// when we shut down, close all of the open tunnels
	defer func() {
		s.RLock()
		defer s.RUnlock()
		for _, t := range s.tunnels {
			go t.Close()
		}
	}()

	for {
		// accept the next proxy connection
		proxy, err := s.raw.Accept()
		if err != nil {
			s.Info("accept failed", "err", err)
			return
		}
		go s.handleProxy(proxy)
	}
}

func (s *session) handleProxy(proxy netx.LoggedConn) {
	proxyError := func(msg string, args ...any) {
		proxy.Error(msg, args...)
		proxy.Close()
	}

	// read out the proxy header
	var proxyHdr proto.ProxyHeader
	err := ReadProxyHeader(proxy, &proxyHdr)
	if err != nil {
		proxyError("error reading proxy header", "err", err)
		return
	}

	// find tunnel
	tunnel, ok := s.getTunnel(proxyHdr.ID)
	if !ok {
		proxyError("no tunnel found for proxy", "id", proxyHdr.ID)
		return
	}

	tunnel.shut.RLock()
	defer tunnel.shut.RUnlock()
	// deliver proxy connection + wrap it so it has a proper RemoteAddr()
	tunnel.handleConn(newProxyConn(proxy, proxyHdr))
}

// Public so we can use it in lib/tunnel/server/functional_test.go
func ReadProxyHeader(proxy netx.LoggedConn, header *proto.ProxyHeader) error {
	var sz int64
	err := binary.Read(proxy, binary.LittleEndian, &sz)
	if err != nil {
		return err
	}

	dec := json.NewDecoder(io.LimitReader(proxy, sz))
	return dec.Decode(&header)
}

func (s *session) unlisten(bindID string) error {
	// delete tunnel
	s.delTunnel(bindID)

	// ask server to unlisten
	resp, err := s.raw.Unlisten(bindID)
	if err != nil {
		return err
	}

	if resp.Error != "" {
		err = errors.New(resp.Error)
		s.Error("server failed to unlisten tunnel", "err", err)
		return err
	}

	return nil
}

func (s *session) getTunnel(id string) (t *tunnel, ok bool) {
	s.RLock()
	defer s.RUnlock()
	t, ok = s.tunnels[id]
	return
}

func (s *session) addTunnel(id string, t *tunnel) {
	s.Lock()
	defer s.Unlock()
	s.tunnels[id] = t
}

func (s *session) delTunnel(id string) {
	s.Lock()
	defer s.Unlock()
	delete(s.tunnels, id)
}

type proxyConn struct {
	netx.LoggedConn
	addr *net.TCPAddr
}

func newProxyConn(conn netx.LoggedConn, hdr proto.ProxyHeader) *ProxyConn {
	pconn := &proxyConn{LoggedConn: conn}

	ip, strport, err := net.SplitHostPort(hdr.ClientAddr)
	if err != nil {
		pconn.addr = &net.TCPAddr{
			IP:   net.ParseIP("0.0.0.0"),
			Port: 0,
		}
	}

	port, err := strconv.Atoi(strport)
	if err != nil {
		port = 0
	}

	pconn.addr = &net.TCPAddr{
		IP:   net.ParseIP(ip),
		Port: port,
	}

	return &ProxyConn{
		Header: hdr,
		Conn:   pconn,
	}
}

func (c *proxyConn) RemoteAddr() net.Addr {
	return c.addr
}
