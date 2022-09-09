package libngrok

import (
	"net"

	"github.com/ngrok/libngrok-go/internal/pb_agent"
)

type CommonConfig[T any] struct {
	parent *T

	CIDRRestrictions *CIDRRestriction
	ProxyProto       ProxyProtoVersion
	Metadata         string
	ForwardsTo       string
}

type ProxyProtoVersion int32

const (
	ProxyProtoV1 = ProxyProtoVersion(1)
	ProxyProtoV2 = ProxyProtoVersion(2)
)

func (cfg *CommonConfig[T]) WithProxyProto(version ProxyProtoVersion) *T {
	cfg.ProxyProto = version
	return cfg.parent
}

func (cfg *CommonConfig[T]) WithMetadata(meta string) *T {
	cfg.Metadata = meta
	return cfg.parent
}

func (cfg *CommonConfig[T]) WithForwardsTo(address string) *T {
	cfg.ForwardsTo = address
	return cfg.parent
}

type CIDRRestriction struct {
	Allowed []string
	Denied  []string
}

func CIDRSet() *CIDRRestriction {
	return &CIDRRestriction{}
}

func (cr *CIDRRestriction) AllowString(cidr ...string) *CIDRRestriction {
	cr.Allowed = append(cr.Allowed, cidr...)
	return cr
}

func (cr *CIDRRestriction) Allow(net ...*net.IPNet) *CIDRRestriction {
	for _, n := range net {
		cr.AllowString(n.String())
	}
	return cr
}

func (cr *CIDRRestriction) DenyString(cidr ...string) *CIDRRestriction {
	cr.Denied = append(cr.Denied, cidr...)
	return cr
}

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

func (cfg *CommonConfig[T]) WithCIDRRestriction(set ...*CIDRRestriction) *T {
	if cfg.CIDRRestrictions == nil {
		cfg.CIDRRestrictions = CIDRSet()
	}

	for _, s := range set {
		if s != nil {
			cfg.CIDRRestrictions.AllowString(s.Allowed...)
			cfg.CIDRRestrictions.DenyString(s.Denied...)
		}
	}
	return cfg.parent
}
