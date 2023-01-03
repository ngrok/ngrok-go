package ngrok

import (
	"context"
	"net"
	"time"

	"golang.ngrok.com/ngrok/config"
	tunnel_client "golang.ngrok.com/ngrok/internal/tunnel/client"
)

// Tunnel is a [net.Listener] created by a call to [Listen] or
// [Session].Listen. A Tunnel allows your application to receive [net.Conn]
// connections from endpoints created on the ngrok service.
type Tunnel interface {
	// Every Tunnel is a net.Listener. It can be plugged into any existing
	// code that expects a net.Listener seamlessly without any changes.
	net.Listener

	// Close is a convenience method for calling Tunnel.CloseWithContext
	// with a context that has a timeout of 5 seconds. This also allows the
	// Tunnel to satisfy the io.Closer interface.
	Close() error
	// CloseWithContext closes the Tunnel. Closing a tunnel is an operation
	// that involves sending a "close" message over the parent session.
	// Since this is a network operation, it is most correct to provide a
	// context with a timeout.
	CloseWithContext(context.Context) error
	// ForwardsTo returns a human-readable string presented in the ngrok
	// dashboard and the Tunnels API. Use config.WithForwardsTo when
	// calling Session.Listen to set this value explicitly.
	ForwardsTo() string
	// ID returns a tunnel's unique ID.
	ID() string
	// Labels returns the labels set by config.WithLabel if this is a
	// labeled tunnel. Non-labeled tunnels will return an empty map.
	Labels() map[string]string
	// Metadata returns the arbitraray metadata string for this tunnel.
	Metadata() string
	// Proto returns the protocol of the tunnel's endpoint.
	// Labeled tunnels will return the empty string.
	Proto() string
	// Session returns the tunnel's parent Session object that it
	// was started on.
	Session() Session
	// URL returns the tunnel endpoint's URL.
	// Labeled tunnels will return the empty string.
	URL() string
}

// Listen creates a new [Tunnel] after connecting a new [Session]. This is a
// shortcut for calling [Connect] then [Session].Listen.
//
// Access to the underlying [Session] that was started automatically can be
// accessed via [Tunnel].Session.
//
// If an error is encoutered during [Session].Listen, the [Session] object that
// was created will be closed automatically.
func Listen(ctx context.Context, tunnelConfig config.Tunnel, connectOpts ...ConnectOption) (Tunnel, error) {
	sess, err := Connect(ctx, connectOpts...)
	if err != nil {
		return nil, err
	}
	tunnel, err := sess.Listen(ctx, tunnelConfig)
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	return tunnel, nil
}

type tunnelImpl struct {
	Sess   Session
	Tunnel tunnel_client.Tunnel
}

func (t *tunnelImpl) Accept() (net.Conn, error) {
	conn, err := t.Tunnel.Accept()
	if err != nil {
		return nil, errAcceptFailed{Inner: err}
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
	return t.Tunnel.Close()
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

type connImpl struct {
	net.Conn
	Proxy *tunnel_client.ProxyConn
}

func (c *connImpl) ProxyConn() *tunnel_client.ProxyConn {
	return c.Proxy
}
