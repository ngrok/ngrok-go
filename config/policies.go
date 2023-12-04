package config

import (
	"golang.ngrok.com/ngrok/internal/pb"
)

type policies struct {
	Inbound  []*policy
	Outbound []*policy
}
type policy struct {
	Name        string
	Expressions []string
	Actions     []*action
}
type action struct {
	Type   string
	Config string
}

type policiesOption func(*policies)
type policyOption func(*policy)
type actionOption func(*action)

type policiesBuilder struct {
	opts []policiesOption
}
type policyBuilder struct {
	opts []policyOption
}
type actionBuilder struct {
	opts []actionOption
}

// Add the provided policies to the ngrok edge
func WithPolicies(opts ...policiesOption) interface {
	HTTPEndpointOption
	TCPEndpointOption
	TLSEndpointOption
} {
	p := &policies{}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Add the provided policy to be applied on inbound connections on an ngrok edge.
// The order in which policies are added is respected at runtime.
func WithInboundPolicy(opts ...policyOption) policiesOption {
	return func(p *policies) {
		policy := &policy{}
		for _, opt := range opts {
			opt(policy)
		}
		p.Inbound = append(p.Inbound, policy)
	}
}

// Add the provided policy to be applied on outbound connections on an ngrok edge.
// The order in which policies are added is respected at runtime.
func WithOutboundPolicy(opts ...policyOption) policiesOption {
	return func(p *policies) {
		policy := &policy{}
		for _, opt := range opts {
			opt(policy)
		}
		p.Outbound = append(p.Outbound, policy)
	}
}

// Add the provided name to this policy
func WithName(name string) policyOption {
	return func(p *policy) {
		p.Name = name
	}
}

// Add the provided cel expression to this policy
func WithExpression(expr string) policyOption {
	return func(p *policy) {
		p.Expressions = append(p.Expressions, expr)
	}
}

// Add the provided action to be executed when this policy's expressions match a connection to an ngrok edge.
// The order in which actions are added to a policy is respected at runtime. At least one action must be specified.
func WithAction(opts ...actionOption) policyOption {
	return func(p *policy) {
		act := &action{}
		for _, opt := range opts {
			opt(act)
		}
		p.Actions = append(p.Actions, act)
	}
}

// Use the provided type for this action. Type must be specified.
func WithType(typ string) actionOption {
	return func(a *action) {
		a.Type = typ
	}
}

// Use the provided json or yaml string as the configuration for this action
func WithConfig(cfg string) actionOption {
	return func(a *action) {
		a.Config = cfg
	}
}

func (p policies) ApplyHTTP(opts *httpOptions) {
	opts.Policies = &p
}

func (p policies) ApplyTCP(opts *tcpOptions) {
	opts.Policies = &p
}

func (p policies) ApplyTLS(opts *tlsOptions) {
	opts.Policies = &p
}

func (p *policies) toProtoConfig() *pb.MiddlewareConfiguration_Policies {
	if p == nil {
		return nil
	}
	inbound := make([]*pb.MiddlewareConfiguration_Policy, len(p.Inbound))
	for i, inP := range p.Inbound {
		inbound[i] = inP.toProtoConfig()
	}

	outbound := make([]*pb.MiddlewareConfiguration_Policy, len(p.Outbound))
	for i, outP := range p.Outbound {
		outbound[i] = outP.toProtoConfig()
	}
	return &pb.MiddlewareConfiguration_Policies{
		Inbound:  inbound,
		Outbound: outbound,
	}
}

func (p *policy) toProtoConfig() *pb.MiddlewareConfiguration_Policy {
	if p == nil {
		return nil
	}

	actions := make([]*pb.MiddlewareConfiguration_Action, len(p.Actions))
	for i, act := range p.Actions {
		actions[i] = act.toProtoConfig()
	}

	return &pb.MiddlewareConfiguration_Policy{Name: p.Name, Expressions: p.Expressions, Actions: actions}
}

func (a *action) toProtoConfig() *pb.MiddlewareConfiguration_Action {
	var cfg []byte
	if a.Config != "" {
		cfg = []byte(a.Config)
	}

	return &pb.MiddlewareConfiguration_Action{Type: a.Type, Config: cfg}
}
