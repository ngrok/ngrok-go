package ngrok

import "golang.ngrok.com/ngrok/internal/tunnel/proto"

// This is the internal-only interface that all config.Tunnel implementations
// *also* implement. This lets us pull the necessary bits out of it without
// polluting the public interface with internal details.
//
// Duplicated from config/tunnel_config.go
type tunnelConfigPrivate interface {
	ForwardsTo() string
	Extra() proto.BindExtra
	Proto() string
	Opts() any
	Labels() map[string]string
	WithForwardsTo(string)
}
