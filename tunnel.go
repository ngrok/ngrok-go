package ngrok

import (
	"context"
	"net"
	"net/http"
	"time"

	tunnel_client "github.com/ngrok/ngrok-go/internal/tunnel/client"
)

// An ngrok tunnel.
type Tunnel interface {
	// Closing a tunnel is an operation that involves sending a "close" message
	// over the existing session. Since this is subject to network latency,
	// packet loss, etc., it is most correct to provide a context. See also
	// `Close`, which matches the `io.Closer` interface method.
	CloseWithContext(context.Context) error
	// Convenience method that calls `CloseWithContext` with a default timeout
	// of 5 seconds.
	Close() error

	// Returns the ForwardsTo string for this tunnel.
	ForwardsTo() string
	// Returns the Metadata string for this tunnel.
	Metadata() string
	// Returns this tunnel's ID.
	ID() string

	// Returns this tunnel's protocol.
	// Will be empty for labeled tunnels.
	Proto() string
	// Returns the URL for this tunnel.
	// Will be empty for labeled tunnels.
	URL() string

	// Returns the labels for this tunnel.
	// Will be empty for non-labeled tunnels.
	Labels() map[string]string

	// Session returns the tunnel's parent Session object that it
	// was started on.
	Session() Session

	// Convert this tunnel to a [net.Listener].
	AsListener() ListenerTunnel
	// Use this tunnel to serve HTTP requests.
	AsHTTP() HTTPTunnel
}

// A tunnel that also implements [net.Listener].
type ListenerTunnel interface {
	Tunnel
	net.Listener
}

// A tunnel that may be used to serve HTTP.
type HTTPTunnel interface {
	Tunnel
	// Serve HTTP requests over this tunnel using the provided [http.Handler].
	Serve(context.Context, http.Handler) error
}

type tunnelImpl struct {
	Sess   Session
	Tunnel tunnel_client.Tunnel
}

func (t *tunnelImpl) Accept() (net.Conn, error) {
	conn, err := t.Tunnel.Accept()
	if err != nil {
		return nil, ErrAcceptFailed{Inner: err}
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

func (t *tunnelImpl) AsHTTP() HTTPTunnel {
	return t
}

func (t *tunnelImpl) AsListener() ListenerTunnel {
	return t
}

func (t *tunnelImpl) Session() Session {
	return t.Sess
}

func (t *tunnelImpl) Serve(ctx context.Context, h http.Handler) error {
	srv := http.Server{
		Handler:     h,
		BaseContext: func(l net.Listener) context.Context { return ctx },
	}
	return srv.Serve(t)
}

// An ngrok tunnel connection.
// For the time being, just a [net.Conn].
// May have additional methods added in the future.
// Must be type-asserted with the [net.Conn] returned from
// [ListenerTunnel].Accept.
type Conn interface {
	net.Conn
}

type connImpl struct {
	net.Conn
	Proxy *tunnel_client.ProxyConn
}

var _ Conn = (*connImpl)(nil)

func (c *connImpl) ProxyConn() *tunnel_client.ProxyConn {
	return c.Proxy
}
