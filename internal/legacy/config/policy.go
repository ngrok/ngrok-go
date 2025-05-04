package config

// No imports needed

type trafficPolicy string

// WithTrafficPolicy configures this edge with the provided policy configuration
// passed as a json or yaml string and overwrites any previously-set traffic policy.
// https://ngrok.com/docs/http/traffic-policy
func WithTrafficPolicy(str string) interface {
	HTTPEndpointOption
	TLSEndpointOption
	TCPEndpointOption
} {
	return trafficPolicy(str)
}

func (p trafficPolicy) ApplyTLS(opts *tlsOptions) {
	opts.TrafficPolicy = string(p)
}

func (p trafficPolicy) ApplyHTTP(opts *httpOptions) {
	opts.TrafficPolicy = string(p)
}

func (p trafficPolicy) ApplyTCP(opts *tcpOptions) {
	opts.TrafficPolicy = string(p)
}
