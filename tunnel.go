package libngrok

import (
	"context"
	"net"
	"net/http"

	tunnel_client "github.com/ngrok/libngrok-go/internal/tunnel/client"
)

type Tunnel interface {
	CloseWithContext(context.Context) error

	ForwardsTo() string
	Metadata() string
	ID() string

	Proto() string
	URL() string

	Labels() map[string]string

	AsListener() ListenerTunnel
	AsHTTP() HTTPTunnel
}

type ListenerTunnel interface {
	Tunnel
	net.Listener
}

type HTTPTunnel interface {
	Tunnel
	Serve(context.Context, http.Handler) error
}

type tunnelImpl struct {
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
	return t.Tunnel.Close()
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

func (t *tunnelImpl) Serve(ctx context.Context, h http.Handler) error {
	srv := http.Server{
		Handler:     h,
		BaseContext: func(l net.Listener) context.Context { return ctx },
	}
	return srv.Serve(t)
}

type Conn interface {
	net.Conn

	// other methods?
}

type connImpl struct {
	net.Conn
	Proxy *tunnel_client.ProxyConn
}
