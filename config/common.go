package config

type commonOpts struct {
	// Restrictions placed on the origin of incoming connections to the edge.
	CIDRRestrictions *cidrRestrictions
	// The version of PROXY protocol to use with this tunnel, zero if not
	// using.
	ProxyProto ProxyProtoVersion
	// Tunnel-specific opaque metadata. Viewable via the API.
	Metadata string
	// Tunnel backend metadata. Viewable via the dashboard and API, but has no
	// bearing on tunnel behavior.

	// The URL to request for this endpoint
	URL string

	// user supplied description of the endpoint
	Description string

	// If not set, defaults to a URI in the format `app://hostname/path/to/executable?pid=12345`
	ForwardsTo string

	// The protocol that's forwarded from the ngrok edge.
	// Currently only relevant for HTTP/1.1 vs HTTP/2, since there's a potential
	// change-of-protocol happening at our edge.
	ForwardsProto string

	// DEPRECATED: use TrafficPolicy instead.
	Policy *policy
	// Policy that define rules that should be applied to incoming or outgoing
	// connections to the edge.
	TrafficPolicy string

	// Enables ingress for ngrok endpoints.
	Bindings []string
}

type CommonOptionsFunc func(cfg *commonOpts)

type CommonOption interface {
	ApplyCommon(cfg *commonOpts)
}

func (of CommonOptionsFunc) ApplyCommon(cfg *commonOpts) {
	of(cfg)
}

func (cfg *commonOpts) getForwardsTo() string {
	if cfg.ForwardsTo == "" {
		return defaultForwardsTo()
	}
	return cfg.ForwardsTo
}

func (cfg *commonOpts) tunnelOptions() {}
