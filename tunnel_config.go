package ngrok

import "github.com/ngrok/ngrok-go/internal/tunnel/proto"

type tunnelConfig struct {
	// Note: Only one set of options should be set at a time - either proto and
	// opts or only labels
	forwardsTo string
	extra      proto.BindExtra

	// HTTP(s), TCP, and TLS tunnels
	proto string
	opts  any

	// Labeled tunnels
	labels map[string]string
}

// Interface implemented by types that can be used to start a tunnel.
type TunnelConfig interface {
	applyTunnelConfig(cfg *tunnelConfig)
}
