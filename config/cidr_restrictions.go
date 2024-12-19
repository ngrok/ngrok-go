package config

import (
	"net"

	"golang.ngrok.com/ngrok/internal/mw"
)

// Restrictions placed on the origin of incoming connections to the edge.
type cidrRestrictions struct {
	// Rejects connections that do not match the given CIDRs
	Allowed []string
	// Rejects connections that match the given CIDRs and allows all other CIDRs.
	Denied []string
}

// Add the provided CIDRS to the [CIDRRestriction].Allowed list.
//
// https://ngrok.com/docs/http/ip-restrictions/
func WithAllowCIDRString(cidr ...string) interface {
	HTTPEndpointOption
	TCPEndpointOption
	TLSEndpointOption
} {
	return &cidrRestrictions{Allowed: cidr}
}

// Add the provided [net.IPNet] to the [CIDRRestriction].Allowed list.
//
// https://ngrok.com/docs/http/ip-restrictions/
func WithAllowCIDR(net ...*net.IPNet) interface {
	HTTPEndpointOption
	TCPEndpointOption
	TLSEndpointOption
} {
	cidrStrings := make([]string, 0, len(net))
	for _, n := range net {
		cidrStrings = append(cidrStrings, n.String())
	}
	return &cidrRestrictions{Allowed: cidrStrings}
}

// Add the provided CIDRS to the [CIDRRestriction].Denied list.
//
// https://ngrok.com/docs/http/ip-restrictions/
func WithDenyCIDRString(cidr ...string) interface {
	HTTPEndpointOption
	TCPEndpointOption
	TLSEndpointOption
} {
	return cidrRestrictions{Denied: cidr}
}

// Add the provided [net.IPNet] to the [CIDRRestriction].Denied list.
//
// https://ngrok.com/docs/http/ip-restrictions/
func WithDenyCIDR(net ...*net.IPNet) interface {
	HTTPEndpointOption
	TCPEndpointOption
	TLSEndpointOption
} {
	cidrStrings := make([]string, 0, len(net))
	for _, n := range net {
		cidrStrings = append(cidrStrings, n.String())
	}
	return cidrRestrictions{Denied: cidrStrings}
}

func (base *cidrRestrictions) merge(set cidrRestrictions) *cidrRestrictions {
	if base == nil {
		base = &cidrRestrictions{}
	}

	base.Allowed = append(base.Allowed, set.Allowed...)
	base.Denied = append(base.Denied, set.Denied...)

	return base
}

func (ir *cidrRestrictions) toProtoConfig() *mw.MiddlewareConfiguration_IPRestriction {
	if ir == nil {
		return nil
	}

	return &mw.MiddlewareConfiguration_IPRestriction{
		AllowCidrs: ir.Allowed,
		DenyCidrs:  ir.Denied,
	}
}

func (opt cidrRestrictions) ApplyHTTP(opts *httpOptions) {
	opts.CIDRRestrictions = opts.CIDRRestrictions.merge(opt)
}

func (opt cidrRestrictions) ApplyTCP(opts *tcpOptions) {
	opts.CIDRRestrictions = opts.CIDRRestrictions.merge(opt)
}

func (opt cidrRestrictions) ApplyTLS(opts *tlsOptions) {
	opts.CIDRRestrictions = opts.CIDRRestrictions.merge(opt)
}
