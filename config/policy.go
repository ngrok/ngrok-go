package config

import (
	"encoding/json"
	"errors"
	"fmt"

	"golang.ngrok.com/ngrok/internal/pb"
	po "golang.ngrok.com/ngrok/policy"
)

type policy po.Policy
type rule po.Rule
type action po.Action

// WithPolicyString configures this edge with the provided policy configuration
// passed as a json string and overwrites any previously-set traffic policy
// https://ngrok.com/docs/http/traffic-policy
func WithPolicyString(jsonStr string) interface {
	HTTPEndpointOption
	TLSEndpointOption
	TCPEndpointOption
} {
	p := &policy{}
	if err := json.Unmarshal([]byte(jsonStr), p); err != nil {
		panic("invalid json for policy configuration")
	}

	return p
}

// WithPolicy configures this edge with the given traffic policy and overwrites any
// previously-set traffic policy
// https://ngrok.com/docs/http/traffic-policy/
func WithPolicy(p po.Policy) interface {
	HTTPEndpointOption
	TLSEndpointOption
	TCPEndpointOption
} {
	ret := policy(p)

	return &ret
}

func (p *policy) ApplyTLS(opts *tlsOptions) {
	opts.Policy = p
}

func (p *policy) ApplyHTTP(opts *httpOptions) {
	opts.Policy = p
}

func (p *policy) ApplyTCP(opts *tcpOptions) {
	opts.Policy = p
}

func (p *policy) toProtoConfig() *pb.MiddlewareConfiguration_Policy {
	if p == nil {
		return nil
	}
	inbound := make([]*pb.MiddlewareConfiguration_PolicyRule, len(p.Inbound))
	for i, inP := range p.Inbound {
		inbound[i] = rule(inP).toProtoConfig()
	}

	outbound := make([]*pb.MiddlewareConfiguration_PolicyRule, len(p.Outbound))
	for i, outP := range p.Outbound {
		outbound[i] = rule(outP).toProtoConfig()
	}
	return &pb.MiddlewareConfiguration_Policy{
		Inbound:  inbound,
		Outbound: outbound,
	}
}

func (pr rule) toProtoConfig() *pb.MiddlewareConfiguration_PolicyRule {
	actions := make([]*pb.MiddlewareConfiguration_PolicyAction, len(pr.Actions))
	for i, act := range pr.Actions {
		actions[i] = action(act).toProtoConfig()
	}

	return &pb.MiddlewareConfiguration_PolicyRule{Name: pr.Name, Expressions: pr.Expressions, Actions: actions}
}

func (a action) toProtoConfig() *pb.MiddlewareConfiguration_PolicyAction {
	var cfgBytes []byte = nil
	if len(a.Config) > 0 {
		var err error
		cfgBytes, err = json.Marshal(a.Config)

		if err != nil {
			panic(errors.New(fmt.Sprintf("failed to parse action configuration due to error: %s", err.Error())))
		}
	}
	return &pb.MiddlewareConfiguration_PolicyAction{
		Type:   a.Type,
		Config: cfgBytes,
	}
}
