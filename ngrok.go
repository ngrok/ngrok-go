package ngrok

import (
	"context"

	"github.com/ngrok/ngrok-go/config"
)

// Create a new ngrok session and start a tunnel.
// Shorthand for a [Connect] followed by a [Session].StartTunnel.
// If an error is encoutered when starting the tunnel, but after a session has
// been established, both the [Session] and error return values will be non-nil.
func ConnectAndStartTunnel(ctx context.Context, connectOpts *ConnectConfig, tunnelOpts config.Tunnel) (Session, Tunnel, error) {
	sess, err := Connect(ctx, connectOpts)
	if err != nil {
		return nil, nil, err
	}

	tun, err := sess.StartTunnel(ctx, tunnelOpts)

	return sess, tun, err
}
