package config

import (
	"encoding/json"
	"fmt"

	"golang.ngrok.com/ngrok/internal/pb"
)

type policy struct {
	Inbound  inboundRules  `json:"inbound,omitempty"`
	Outbound outboundRules `json:"outbound,omitempty"`
}

type inboundRules []policyRule
type outboundRules []policyRule

type policyRule struct {
	Name        string   `json:"name,omitempty"`
	Expressions []string `json:"expressions,omitempty"`
	Actions     []action `json:"actions"`
}
type action struct {
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config,omitempty"`
}

type policyActionOption = option[*action]
type policyRuleOption = option[*policyRule]
type policyRuleSetOption = option[*[]policyRule]
type policyOption = option[*policy]

// Supports conversion to a json string
type JsonConvertible interface {
	ToJSON() string
}

func (p *policy) ToJSON() string {
	return toJSON(p)
}

func (p policyRule) ToJSON() string {
	return toJSON(p)
}

func (p action) ToJSON() string {
	return toJSON(p)
}

func toJSON(o any) string {
	bytes, err := json.Marshal(o)

	if err != nil {
		panic(fmt.Sprintf("failed to convert to json with error: %s", err.Error()))
	}

	return string(bytes)
}

// WithPolicyConfig configures this edge with the provided policy configuration
// passed as a json string and overwrites any prevously-set traffic policy
// https://ngrok.com/docs/http/traffic-policy
func WithPolicyConfig(jsonStr string) interface {
	HTTPEndpointOption
	TLSEndpointOption
	TCPEndpointOption
	JsonConvertible
} {
	p := &policy{}
	if err := json.Unmarshal([]byte(jsonStr), p); err != nil {
		panic("invalid json for policy configuration")
	}

	return p
}

// WithPolicy configures this edge with the given traffic and overwrites any
// previously-set traffic policy
// https://ngrok.com/docs/http/traffic-policy/
func WithPolicy(opts ...policyOption) interface {
	HTTPEndpointOption
	TLSEndpointOption
	TCPEndpointOption
	JsonConvertible
} {
	cfg := &policy{}
	applyOpts(cfg, opts...)

	return cfg
}

// WithInboundRules adds the provided policy rules to the inbound
// set of the given policy.
// The order in which policies are specified is observed.
func WithInboundRules(opts ...policyRuleSetOption) policyOption {
	rules := []policyRule{}
	applyOpts(&rules, opts...)

	return inboundRules(rules)
}

// WithOutboundRules adds the provided policy to be outbound
// set of the given policy.
// The order in which policies are specified is observed.
func WithOutboundRules(opts ...policyRuleSetOption) policyOption {
	rules := []policyRule{}
	applyOpts(&rules, opts...)

	return outboundRules(rules)
}

// WithPolicyRule provides a policy rule built from the given options.
func WithPolicyRule(opts ...policyRuleOption) interface {
	policyRuleSetOption
	JsonConvertible
} {
	pr := policyRule{}
	applyOpts(&pr, opts...)

	return pr
}

// WithPolicyName sets the provided name on a policy rule.
func WithPolicyName(n string) policyRuleOption {
	return optionFunc[*policyRule](
		func(r *policyRule) {
			r.Name = n
		})
}

// WithPolicyExpression appends the provided CEL statement to a policy rule's expressions.
func WithPolicyExpression(e string) policyRuleOption {
	return optionFunc[*policyRule](
		func(r *policyRule) {
			r.Expressions = append(r.Expressions, e)
		})
}

// WithPolicyAction appends the provided action to the set of the policy rule.
// The order the actions are specified is observed.
func WithPolicyAction(opts ...policyActionOption) interface {
	policyRuleOption
	JsonConvertible
} {
	a := action{}
	applyOpts(&a, opts...)

	return a
}

// WithActionType sets the provided type for this action. Type must be specified.
func WithPolicyActionType(t string) policyActionOption {
	return optionFunc[*action](func(a *action) { a.Type = t })
}

// WithConfig sets the provided map as the configuration for this action
func WithPolicyActionConfig(c map[string]any) policyActionOption {
	return optionFunc[*action](
		func(a *action) {
			raw, err := json.Marshal(c)
			if err != nil {
				panic("unable to marshal action config")
			}
			a.Config = raw
		})
}

// supports inbound rules as an a policy option
func (ib inboundRules) apply(p *policy) {
	p.Inbound = append(p.Inbound, ib...)
}

// supports outbound rules as a policy option
func (ib outboundRules) apply(p *policy) {
	p.Outbound = append(p.Outbound, ib...)
}

// supports policy rule as an option of a collection of
// rules, which can be used for inbound or outbound
func (pr policyRule) apply(r *[]policyRule) {
	*r = append(*r, pr)
}

// supports action as an option of a policy rule
func (a action) apply(p *policyRule) {
	p.Actions = append(p.Actions, a)
}

// an option that is applicable to the specified type
type option[T any] interface {
	apply(T)
}

type optionFunc[T any] func(T)

func (f optionFunc[T]) apply(a T) {
	f(a)
}

// applies the set of options to the specified target
func applyOpts[T any](target T, opts ...option[T]) {
	for _, o := range opts {
		o.apply(target)
	}
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
		inbound[i] = inP.toProtoConfig()
	}

	outbound := make([]*pb.MiddlewareConfiguration_PolicyRule, len(p.Outbound))
	for i, outP := range p.Outbound {
		outbound[i] = outP.toProtoConfig()
	}
	return &pb.MiddlewareConfiguration_Policy{
		Inbound:  inbound,
		Outbound: outbound,
	}
}

func (pr policyRule) toProtoConfig() *pb.MiddlewareConfiguration_PolicyRule {
	actions := make([]*pb.MiddlewareConfiguration_PolicyAction, len(pr.Actions))
	for i, act := range pr.Actions {
		actions[i] = act.toProtoConfig()
	}

	return &pb.MiddlewareConfiguration_PolicyRule{Name: pr.Name, Expressions: pr.Expressions, Actions: actions}
}

func (a action) toProtoConfig() *pb.MiddlewareConfiguration_PolicyAction {
	var cfg []byte
	if len(a.Config) > 0 {
		cfg = a.Config
	}

	return &pb.MiddlewareConfiguration_PolicyAction{Type: a.Type, Config: cfg}
}
