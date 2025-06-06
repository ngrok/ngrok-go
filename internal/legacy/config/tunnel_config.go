package config

import (
	"net/url"

	"golang.ngrok.com/ngrok/v2/internal/tunnel/proto"
)

// Tunnel is a marker interface for options that can be used to start
// tunnels.
// It should not be implemented outside of this module.
type Tunnel interface {
	tunnelOptions()
}

// This is the internal-only interface that all Tunnel implementations *also*
// implement. This lets us pull the necessary bits out of it without polluting
// the public interface with internal details.
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
