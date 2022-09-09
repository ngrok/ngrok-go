package client

import (
	"errors"
	"net"
	"net/url"
	"sync/atomic"

	"github.com/ngrok/libngrok-go/internal/tunnel/proto"
)

type Tunnel interface {
	Accept() (*ProxyConn, error)
	Addr() net.Addr
	Close() error
	RemoteBindConfig() *RemoteBindConfig
	ID() string
	ForwardsTo() string
}

type ProxyConn struct {
	Header proto.ProxyHeader
	Conn   net.Conn
}

// A Tunnel is a net.Listener that Accept()'s connections from a
// remote machine.
type tunnel struct {
	id          atomic.Value
	configProto string
	url         string
	opts        any
	token       string
	bindExtra   proto.BindExtra
	labels      map[string]string
	forwardsTo  string

	accept   chan *ProxyConn // new connections come on this channel
	unlisten func() error    // call this function to close the tunnel

	shut shutdown // for clean shutdowns
}

func newTunnel(resp proto.BindResp, extra proto.BindExtra, s *session, forwardsTo string) *tunnel {
	id := atomic.Value{}
	id.Store(resp.ClientID)
	return &tunnel{
		id:          id,
		configProto: resp.Proto,
		url:         resp.URL,
		opts:        resp.Opts,
		token:       resp.Extra.Token,
		bindExtra:   extra, // this makes the reconnecting session a little easier
		accept:      make(chan *ProxyConn),
		unlisten:    func() error { return s.unlisten(resp.ClientID) },
		forwardsTo:  forwardsTo,
	}
}

func newTunnelLabel(resp proto.StartTunnelWithLabelResp, metadata string, labels map[string]string, s *session, forwardsTo string) *tunnel {
	id := atomic.Value{}
	id.Store(resp.ID)
	return &tunnel{
		id: id,
		bindExtra: proto.BindExtra{
			Metadata: metadata,
		}, // this makes the reconnecting session a little easier
		labels:     labels,
		accept:     make(chan *ProxyConn),
		unlisten:   func() error { return s.unlisten(resp.ID) },
		forwardsTo: forwardsTo,
	}
}

func (t *tunnel) handleConn(r *ProxyConn) {
	t.shut.Do(func() {
		t.accept <- r
	})
}

// Accept returns the next available connection from a remote machine, or an
// error if the tunnel closes.
func (t *tunnel) Accept() (*ProxyConn, error) {
	conn, ok := <-t.accept
	if !ok {
		return nil, errors.New("Tunnel closed")
	}
	return conn, nil
}

// Closes the Tunnel by asking the remote machine to deallocate its listener, or
// an error if the request failed.
func (t *tunnel) Close() (err error) {
	t.shut.Shut(func() {
		err = t.unlisten()
		close(t.accept)
	})
	return
}

// Addr returns the address of the public endpoint of the tunnel listener on the
// remote machine.
func (t *tunnel) Addr() net.Addr {
	return t.RemoteBindConfig()
}

// ForwardsTo returns the address of the upstream the ngrok agent will
// forward proxied connections to
func (t *tunnel) ForwardsTo() string {
	return t.forwardsTo
}

func (t *tunnel) ID() string {
	return t.id.Load().(string)
}

// RemoteBindConfig returns more detailed information about the public endpoint of the
// tunnel listener on the remote machine.
func (t *tunnel) RemoteBindConfig() *RemoteBindConfig {
	return &RemoteBindConfig{
		URL:         t.url,
		ConfigProto: t.configProto,
		Opts:        t.opts,
		Token:       t.token,
		Metadata:    t.bindExtra.Metadata,
		Labels:      t.labels,
	}
}

type RemoteBindConfig struct {
	URL         string // url assigned at bind. empty string for label tunnels
	ConfigProto string // proto if one was specified for the tunnel at startup. empty string for label tunnels
	Opts        any
	Token       string
	Metadata    string
	Labels      map[string]string
}

func (a *RemoteBindConfig) Network() string {
	return "tcp"
}

func (a *RemoteBindConfig) String() string {
	u, err := url.Parse(a.URL)
	if err != nil {
		panic(err)
	}
	return u.Host
}
