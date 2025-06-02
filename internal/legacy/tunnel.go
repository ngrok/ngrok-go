package legacy

import (
	"context"
	"net"
	"net/http"
	"time"

	tunnel_client "golang.ngrok.com/ngrok/v2/internal/tunnel/client"
)

// Tunnel is a [net.Listener] created by a call to [Listen] or
// [Session].Listen. A Tunnel allows your application to receive [net.Conn]
// connections from endpoints created on the ngrok service.
type Tunnel interface {
	// Every Tunnel is a net.Listener. It can be plugged into any existing
	// code that expects a net.Listener seamlessly without any changes.
	net.Listener

	// Information associated with the tunnel
	TunnelInfo

	// Close is a convenience method for calling Tunnel.CloseWithContext
	// with a context that has a timeout of 5 seconds. This also allows the
	// Tunnel to satisfy the io.Closer interface.
	Close() error

	// CloseWithContext closes the Tunnel. Closing a tunnel is an operation
	// that involves sending a "close" message over the parent session.
	// Since this is a network operation, it is most correct to provide a
	// context with a timeout.
	CloseWithContext(context.Context) error

	// Session returns the tunnel's parent Session object that it
	// was started on.
	Session() Session
}

// TunnelInfo implementations contain metadata about a [Tunnel].
type TunnelInfo interface {
	// ForwardsTo returns a human-readable string presented in the ngrok
	// dashboard and the Tunnels API. Use config.WithForwardsTo when
	// calling Session.Listen to set this value explicitly.
	ForwardsTo() string
	// ID returns a tunnel's unique ID.
	ID() string
	// Labels returns the labels set by config.WithLabel if this is a
	// labeled tunnel. Non-labeled tunnels will return an empty map.
	Labels() map[string]string
	// Metadata returns the arbitrary metadata string for this tunnel.
	Metadata() string
	// Proto returns the protocol of the tunnel's endpoint.
	// Labeled tunnels will return the empty string.
	Proto() string
	// URL returns the tunnel endpoint's URL.
	// Labeled tunnels will return the empty string.
	URL() string
}

type tunnelImpl struct {
	Sess   Session
	Tunnel tunnel_client.Tunnel
	server *http.Server
}

func (t *tunnelImpl) Accept() (net.Conn, error) {
	conn, err := t.Tunnel.Accept()
	if err != nil {
		err = errAcceptFailed{Inner: err}
		if s, ok := t.Sess.(*sessionImpl); ok {
			if si := s.inner(); si != nil {
				si.Logger.Info(err.Error(), "clientid", t.Tunnel.ID())
			}
		}
		return nil, err
	}
	return &connImpl{
		Conn:  conn.Conn,
		Proxy: conn,
	}, nil
}

func (t *tunnelImpl) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	return t.CloseWithContext(ctx)
}

func (t *tunnelImpl) CloseWithContext(_ context.Context) error {
	if t.server != nil {
		err := t.server.Close()
		if err != nil {
			return err
		}
	}
	err := t.Tunnel.Close()
	return err
}

func (t *tunnelImpl) Addr() net.Addr {
	return t.Tunnel.Addr()
}

func (t *tunnelImpl) URL() string {
	return t.Tunnel.RemoteBindConfig().URL
}

func (t *tunnelImpl) Proto() string {
	return t.Tunnel.RemoteBindConfig().ConfigProto
}

func (t *tunnelImpl) ForwardsProto() string {
	return t.Tunnel.ForwardsProto()
}

func (t *tunnelImpl) ForwardsTo() string {
	return t.Tunnel.ForwardsTo()
}

func (t *tunnelImpl) Metadata() string {
	return t.Tunnel.RemoteBindConfig().Metadata
}

func (t *tunnelImpl) ID() string {
	return t.Tunnel.ID()
}

func (t *tunnelImpl) Labels() map[string]string {
	return t.Tunnel.RemoteBindConfig().Labels
}

func (t *tunnelImpl) Session() Session {
	return t.Sess
}

// Conn is a connection from an ngrok [Tunnel].
//
// It implements the standard [net.Conn] interface and has additional methods
// to query ngrok-specific connection metadata.
//
// Because the [net.Listener] interface requires `Accept` to return a
// [net.Conn], you will have to type-assert it to an ngrok [Conn]:
// ```
// conn, _ := tun.Accept()
// ngrokConn := conn.(ngrok.Conn)
// ```
type Conn interface {
	net.Conn
	// Proto returns the tunnel protocol (http, https, tls, or tcp) for this connection.
	Proto() string

	// PassthroughTLS returns whether this connection contains an end-to-end tls
	// connection.
	PassthroughTLS() bool
}

type connImpl struct {
	net.Conn
	Proxy *tunnel_client.ProxyConn
}

// compile-time check that we're implementing the proper interface
var _ Conn = &connImpl{}

func (c *connImpl) ProxyConn() *tunnel_client.ProxyConn {
	return c.Proxy
}

func (c *connImpl) Proto() string {
	return c.Proxy.Header.Proto
}

func (c *connImpl) PassthroughTLS() bool {
	return c.Proxy.Header.PassthroughTLS
}
