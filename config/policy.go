package config

import (
	"encoding/json"
)

type Policy any
type withPolicy struct {
	policy Policy
}

// WithPolicyString configures this edge with the provided policy configuration
// passed as a json string and overwrites any previously-set traffic policy
// https://ngrok.com/docs/http/traffic-policy
func WithPolicyString(jsonStr string) interface {
	HTTPEndpointOption
	TLSEndpointOption
	TCPEndpointOption
} {
	p := map[string]any{}
	if err := json.Unmarshal([]byte(jsonStr), &p); err != nil {
		panic("invalid json for policy configuration")
	}

	return WithPolicy(Policy(p))
}

// WithPolicy configures this edge with the given traffic policy and overwrites any
// previously-set traffic policy
// https://ngrok.com/docs/http/traffic-policy/
func WithPolicy(p Policy) interface {
	HTTPEndpointOption
	TLSEndpointOption
	TCPEndpointOption
} {
	return withPolicy{p}
}

func (p withPolicy) ApplyTLS(opts *tlsOptions) {
	opts.Policy = p.policy
}

func (p withPolicy) ApplyHTTP(opts *httpOptions) {
	opts.Policy = p.policy
}

func (p withPolicy) ApplyTCP(opts *tcpOptions) {
	opts.Policy = p.policy
}
