package libngrok

import "context"

func ConnectAndStartTunnel(ctx context.Context, connectOpts *ConnectConfig, tunnelOpts TunnelConfig) (Session, Tunnel, error) {
	sess, err := Connect(ctx, connectOpts)
	if err != nil {
		return nil, nil, err
	}

	tun, err := sess.StartTunnel(ctx, tunnelOpts)

	return sess, tun, err
}
