package policy

import (
	"bytes"
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v2"
)

type Policy struct {
	// the ordered set of rules that apply to inbound traffic
	Inbound []Rule `json:"inbound,omitempty" yaml:"inbound,omitempty"`
	// the ordered set of rules that apply to outbound traffic
	Outbound []Rule `json:"outbound,omitempty" yaml:"outbound,omitempty"`
}

type Rule struct {
	// the name of the traffic policy rule
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	// the set of CEL expressions used to determine if this rule is applicable
	Expressions []string `json:"expressions,omitempty" yaml:"expressions,omitempty"`
	// the ordered set of actions that should take effect against the traffic
	Actions []Action `json:"actions" yaml:"actions"`
}

type Action struct {
	// the type of action that should be used
	Type string `json:"type" yaml:"type"`
	// the configuration for the specified action type written as a json string
	Config map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}

// converts the policy to a json string
func (p Policy) JSON() (string, error) {
	return marshalJSON(p)
}

// converts the policy to a yaml string
func (p Policy) YAML() (string, error) {
	return marshalYAML(p)
}

// creates a rule from the specified string in json or yaml format
func NewRuleFromString(input string) (Rule, error) {
	r := Rule{}
	err := unmarshal(input, &r)

	return r, err
}

// creates a rule from the specified string in json or yaml format and panics if invalid
func MustRuleFromString(input string) Rule {
	r := Rule{}
	if err := unmarshal(input, &r); err != nil {
		panic(fmt.Sprintf("failed to create rule from specified string due to error: %s", err.Error()))
	}

	return r
}

// converts the rule to a json string
func (p Rule) JSON() (string, error) {
	return marshalJSON(p)
}

// converts the rule to a yaml string
func (p Rule) YAML() (string, error) {
	return marshalYAML(p)
}

// creates an action from the specified string in json or yaml format
func NewActionFromString(input string) (Action, error) {
	a := Action{}
	err := unmarshal(input, &a)

	return a, err
}

// creates an action from the specified string in json or yaml format and panics if invalid
func MustActionFromString(input string) Action {
	a := Action{}
	if err := unmarshal(input, &a); err != nil {
		panic(fmt.Sprintf("failed to create action from specified string due to error: %s", err.Error()))
	}

	return a
}

// converts the action to a json string
func (p Action) JSON() (string, error) {
	return marshalJSON(p)
}

// converts the action to a yaml string
func (p Action) YAML() (string, error) {
	return marshalYAML(p)
}

func marshalJSON(o any) (string, error) {
	b := new(bytes.Buffer)
	enc := json.NewEncoder(b)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(o); err != nil {
		return "", err
	}

	return b.String(), nil
}

func marshalYAML(o any) (string, error) {
	bytes, err := yaml.Marshal(o)

	if err != nil {
		return "", err
	}

	return string(bytes), nil
}

func unmarshal(input string, typ any) error {
	return yaml.UnmarshalStrict([]byte(input), typ)
}
