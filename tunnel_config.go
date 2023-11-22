package ngrok

import (
	"net/url"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

// This is the internal-only interface that all config.Tunnel implementations
// *also* implement. This lets us pull the necessary bits out of it without
// polluting the public interface with internal details.
//
// Duplicated from config/tunnel_config.go
type tunnelConfigPrivate interface {
	ForwardsTo() string
	ForwardsProto() string
	Extra() proto.BindExtra
	Proto() string
	Opts() any
	Labels() map[string]string
	// Extra config when auto-forwarding to a URL.
	// Normal operation should use the functional builder.
	WithForwardsTo(*url.URL)
}
