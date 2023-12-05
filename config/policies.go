package config

import (
	"golang.ngrok.com/ngrok/internal/pb"
)

type inboundPolicy struct {
	*policy
}
type outboundPolicy struct {
	*policy
}

type policies struct {
	Inbound  []inboundPolicy
	Outbound []outboundPolicy
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

// WithInboundPolicy adds the provided policy to be applied on inbound connections on an ngrok edge.
// The order in which policies are added is respected at runtime.
func WithInboundPolicy(opts ...PolicyOption) interface {
	HTTPEndpointOption
	TLSEndpointOption
	TCPEndpointOption
} {
	inP := &policy{}
	for _, opt := range opts {
		opt(inP)
	}

	return inboundPolicy{inP}
}

// WithOutboundPolicy adds the provided policy to be applied on outbound connections on an ngrok edge.
// The order in which policies are added is respected at runtime.
func WithOutboundPolicy(opts ...PolicyOption) interface {
	HTTPEndpointOption
	TLSEndpointOption
	TCPEndpointOption
} {
	outP := &policy{}
	for _, opt := range opts {
		opt(outP)
	}

	return outboundPolicy{outP}
}

// WithPolicyName sets the provided name on this policy
func WithPolicyName(name string) PolicyOption {
	return func(p *policy) {
		p.Name = name
	}
}

// WithExpressions appends the provided cel expression to this policy
func WithPolicyExpression(expr string) PolicyOption {
	return func(p *policy) {
		p.Expressions = append(p.Expressions, expr)
	}
}

// WithPolicyAction appends the provided action to be executed when this policy's expressions match a connection to an ngrok edge.
// The order in which actions are added to a policy is respected at runtime. At least one action must be specified.
func WithPolicyAction(opts ...ActionOption) PolicyOption {
	return func(p *policy) {
		act := &action{}
		for _, opt := range opts {
			opt(act)
		}
		p.Actions = append(p.Actions, act)
	}
}

// WithActionType sets the provided type for this action. Type must be specified.
func WithActionType(typ string) ActionOption {
	return func(a *action) {
		a.Type = typ
	}
}

// WithActionConfig sets the provided json or yaml string as the configuration for this action
func WithActionConfig(cfg string) ActionOption {
	return func(a *action) {
		a.Config = cfg
	}
}

func (ip inboundPolicy) ApplyHTTP(opts *httpOptions) {
	if opts.Policies == nil {
		opts.Policies = &policies{}
	}
	opts.Policies.Inbound = append(opts.Policies.Inbound, ip)
}

func (ip inboundPolicy) ApplyTCP(opts *tcpOptions) {
	if opts.Policies == nil {
		opts.Policies = &policies{}
	}
	opts.Policies.Inbound = append(opts.Policies.Inbound, ip)
}

func (ip inboundPolicy) ApplyTLS(opts *tlsOptions) {
	if opts.Policies == nil {
		opts.Policies = &policies{}
	}
	opts.Policies.Inbound = append(opts.Policies.Inbound, ip)
}

func (op outboundPolicy) ApplyHTTP(opts *httpOptions) {
	if opts.Policies == nil {
		opts.Policies = &policies{}
	}
	opts.Policies.Outbound = append(opts.Policies.Outbound, op)
}

func (op outboundPolicy) ApplyTCP(opts *tcpOptions) {
	if opts.Policies == nil {
		opts.Policies = &policies{}
	}
	opts.Policies.Outbound = append(opts.Policies.Outbound, op)
}

func (op outboundPolicy) ApplyTLS(opts *tlsOptions) {
	if opts.Policies == nil {
		opts.Policies = &policies{}
	}
	opts.Policies.Outbound = append(opts.Policies.Outbound, op)
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
