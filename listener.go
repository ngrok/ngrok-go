package ngrok

import (
	"context"
	"crypto/tls"
	"net"

	"golang.ngrok.com/ngrok/v2/internal/legacy"
)

// EndpointListener is an endpoint that you may treat as a net.Listener.
type EndpointListener interface {
	Endpoint

	// Accept returns the next connection received the Endpoint.
	Accept() (net.Conn, error)

	// Addr() returns where the Endpoint is listening.
	Addr() net.Addr
}

// endpointListener implements the EndpointListener interface.
type endpointListener struct {
	baseEndpoint
	tunnel legacy.Tunnel
}

// wrapConnWithTLS is a wrapper around a net.Conn that performs TLS termination
// without immediately performing the handshake
func wrapConnWithTLS(conn net.Conn, tlsConfig *tls.Config) net.Conn {
	if tlsConfig == nil {
		return conn
	}

	// Create a TLS server connection without performing handshake
	// The handshake will happen when the client first reads or writes
	return tls.Server(conn, tlsConfig)
}

func (e *endpointListener) Accept() (net.Conn, error) {
	// Accept connection from the tunnel
	conn, err := e.tunnel.Accept()
	if err != nil {
		return nil, wrapError(err)
	}

	// Apply TLS termination if a config is provided
	if e.agentTLSConfig != nil {
		// Wrap the connection with TLS without performing handshake
		return wrapConnWithTLS(conn, e.agentTLSConfig), nil
	}

	// Return the raw connection if no TLS certificate is provided
	return conn, nil
}

func (e *endpointListener) Addr() net.Addr {
	return e.tunnel.Addr()
}

func (e *endpointListener) Close() error {
	return e.CloseWithContext(context.Background())
}

func (e *endpointListener) CloseWithContext(ctx context.Context) error {
	err := e.tunnel.CloseWithContext(ctx)
	e.signalDone()

	// Remove from agent
	if a, ok := e.agent.(*agent); ok {
		a.removeEndpoint(e)
	}

	return wrapError(err)
}
