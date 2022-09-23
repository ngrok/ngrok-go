package ngrok

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/ngrok/ngrok-go/internal/pb_agent"
)

type CommonConfig struct {
	// Restrictions placed on the origin of incoming connections to the edge.
	CIDRRestrictions *CIDRRestriction
	// The version of PROXY protocol to use with this tunnel, zero if not
	// using.
	ProxyProto ProxyProtoVersion
	// Tunnel-specific opaque metadata. Viewable via the API.
	Metadata string
	// Tunnel backend metadata. Viewable via the dashboard and API, but has no
	// bearing on tunnel behavior.
	// If not set, defaults to a URI in the format `app://hostname/path/to/executable?pid=12345`
	ForwardsTo string
}

// A valid PROXY protocol version
type ProxyProtoVersion int32

const (
	// PROXY protocol disabled
	ProxyProtoNone = ProxyProtoVersion(0)
	// PROXY protocol v1
	ProxyProtoV1 = ProxyProtoVersion(1)
	// PROXY protocol v2
	ProxyProtoV2 = ProxyProtoVersion(2)
)

// Use the provided PROXY protocol version for connections over this tunnel.
// Sets the [CommonConfig].ProxyProto field.
func (cfg *CommonConfig) WithProxyProto(version ProxyProtoVersion) *CommonConfig {
	cfg.ProxyProto = version
	return cfg
}

// Use the provided opaque metadata string for this tunnel.
// Sets the [CommonConfig].Metadata field.
func (cfg *CommonConfig) WithMetadata(meta string) *CommonConfig {
	cfg.Metadata = meta
	return cfg
}

// Use the provided backend as the tunnel's ForwardsTo string.
// Sets the [CommonConfig].ForwardsTo field.
func (cfg *CommonConfig) WithForwardsTo(backend string) *CommonConfig {
	cfg.ForwardsTo = backend
	return cfg
}

// Restrictions placed on the origin of incoming connections to the edge.
type CIDRRestriction struct {
	// Rejects connections that do not match the given CIDRs
	Allowed []string
	// Rejects connections that match the given CIDRs and allows all other CIDRs.
	Denied []string
}

// Construct a new set of [CIDRRestriction]
func CIDRSet() *CIDRRestriction {
	return &CIDRRestriction{}
}

// Add the provided CIDRS to the [CIDRRestriction].Allowed list.
func (cr *CIDRRestriction) AllowString(cidr ...string) *CIDRRestriction {
	cr.Allowed = append(cr.Allowed, cidr...)
	return cr
}

// Add the provided [net.IPNet] to the [CIDRRestriction].Allowed list.
func (cr *CIDRRestriction) Allow(net ...*net.IPNet) *CIDRRestriction {
	for _, n := range net {
		cr.AllowString(n.String())
	}
	return cr
}

// Add the provided CIDRS to the [CIDRRestriction].Denied list.
func (cr *CIDRRestriction) DenyString(cidr ...string) *CIDRRestriction {
	cr.Denied = append(cr.Denied, cidr...)
	return cr
}

// Add the provided [net.IPNet] to the [CIDRRestriction].Denied list.
func (cr *CIDRRestriction) Deny(net ...*net.IPNet) *CIDRRestriction {
	for _, n := range net {
		cr.DenyString(n.String())
	}
	return cr
}

func (ir *CIDRRestriction) toProtoConfig() *pb_agent.MiddlewareConfiguration_IPRestriction {
	if ir == nil {
		return nil
	}

	return &pb_agent.MiddlewareConfiguration_IPRestriction{
		AllowCIDRs: ir.Allowed,
		DenyCIDRs:  ir.Denied,
	}
}

// Add the provided [CIDRRestriction] to the tunnel.
// Concatenates all provided Allowed and Denied lists with the existing ones.
func (cfg *CommonConfig) WithCIDRRestriction(set ...*CIDRRestriction) *CommonConfig {
	if cfg.CIDRRestrictions == nil {
		cfg.CIDRRestrictions = CIDRSet()
	}

	for _, s := range set {
		if s != nil {
			cfg.CIDRRestrictions.AllowString(s.Allowed...)
			cfg.CIDRRestrictions.DenyString(s.Denied...)
		}
	}
	return cfg
}

func defaultForwardsTo() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "<unknown>"
	}

	exe, err := os.Executable()
	if err != nil {
		exe = "<unknown>"
	} else {
		exe = filepath.ToSlash(exe)
	}

	pid := os.Getpid()

	return fmt.Sprintf("app://%s/%s?pid=%d", hostname, exe, pid)
}

func (cfg CommonConfig) applyTunnelConfig(tcfg *tunnelConfig) {
	if cfg.ForwardsTo == "" {
		tcfg.forwardsTo = defaultForwardsTo()
	} else {
		tcfg.forwardsTo = cfg.ForwardsTo
	}
	tcfg.extra.Metadata = cfg.Metadata
}
