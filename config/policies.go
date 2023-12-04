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
func NewPolicies(opts ...policiesOption) *policies {
	p := &policies{}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Add the provided policy to be applied on inbound connections on an ngrok edge.
// The order in which policies are added is respected at runtime.
func (p *policies) WithInboundPolicy(in *policy) *policies {
	p.Inbound = append(p.Inbound, in)
	return p
}

// Add the provided policy to be applied on outbound connections on an ngrok edge.
// The order in which policies are added is respected at runtime.
func (p *policies) WithOutboundPolicy(out *policy) *policies {
	p.Outbound = append(p.Outbound, out)
	return p
}

// Creates a builder for a policy
func NewPolicy() *policy {
	return &policy{}
}

// Add the provided name to this policy
func (p *policy) WithName(name string) *policy {
	p.Name = name
	return p
}

// Add the provided cel expression to this policy
func (p *policy) WithExpression(expr string) *policy {
	p.Expressions = append(p.Expressions, expr)
	return p
}

// Add the provided action to be executed when this policy's expressions match a connection to an ngrok edge.
// The order in which actions are added to a policy is respected at runtime. At least one action must be specified.
func (p *policy) WithAction(act *action) *policy {
	p.Actions = append(p.Actions, act)
	return p
}

// Creates a builder for an action
func NewAction() *action {
	return &action{}
}

// Use the provided type for this action. Type must be specified.
func (a *action) WithType(typ string) *action {
	a.Type = typ
	return a
}

// Use the provided json or yaml string as the configuration for this action
func (a *action) WithConfig(cfg string) *action {
	a.Config = cfg
	return a
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
