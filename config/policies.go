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

type PoliciesOption func(*policies)
type PolicyOption func(*policy)
type ActionOption func(*action)

// WithPolicies creates a new set of policies with the provided options
func WithPolicies(opts ...PoliciesOption) interface {
	HTTPEndpointOption
	TLSEndpointOption
	TCPEndpointOption
} {
	p := &policies{}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// WithInboundPolicy adds the provided policy to be applied on inbound connections on an ngrok edge.
// The order in which policies are added is respected at runtime.
func WithInboundPolicy(opts ...PolicyOption) PoliciesOption {
	return func(p *policies) {
		inP := &policy{}
		for _, opt := range opts {
			opt(inP)
		}

		p.Inbound = append(p.Inbound, inP)
	}
}

// WithOutboundPolicy adds the provided policy to be applied on outbound connections on an ngrok edge.
// The order in which policies are added is respected at runtime.
func WithOutboundPolicy(opts ...PolicyOption) PoliciesOption {
	return func(p *policies) {
		outP := &policy{}
		for _, opt := range opts {
			opt(outP)
		}
		p.Outbound = append(p.Outbound, outP)
	}
}

// WithName sets the provided name on this policy
func WithName(name string) PolicyOption {
	return func(p *policy) {
		p.Name = name
	}
}

// WithExpressions appends the provided cel expression to this policy
func WithExpression(expr string) PolicyOption {
	return func(p *policy) {
		p.Expressions = append(p.Expressions, expr)
	}
}

// WithAction appends the provided action to be executed when this policy's expressions match a connection to an ngrok edge.
// The order in which actions are added to a policy is respected at runtime. At least one action must be specified.
func WithAction(opts ...ActionOption) PolicyOption {
	return func(p *policy) {
		act := &action{}
		for _, opt := range opts {
			opt(act)
		}
		p.Actions = append(p.Actions, act)
	}
}

// WithType sets the provided type for this action. Type must be specified.
func WithType(typ string) ActionOption {
	return func(a *action) {
		a.Type = typ
	}
}

// WithConfig sets the provided json or yaml string as the configuration for this action
func WithConfig(cfg string) ActionOption {
	return func(a *action) {
		a.Config = cfg
	}
}

func (p *policies) ApplyHTTP(opts *httpOptions) {
	opts.Policies = p
}

func (p *policies) ApplyTCP(opts *tcpOptions) {
	opts.Policies = p
}

func (p *policies) ApplyTLS(opts *tlsOptions) {
	opts.Policies = p
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
