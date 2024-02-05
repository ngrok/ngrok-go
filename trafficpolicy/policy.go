package trafficpolicy

import (
	"bytes"
	"encoding/json"

	"gopkg.in/yaml.v3"
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
	Config ActionConfigString `json:"config,omitempty" yaml:"config,omitempty"`
}

type ActionConfigString string

func (s ActionConfigString) MarshalJSON() ([]byte, error) {
	raw := json.RawMessage([]byte(s))

	return json.Marshal(raw)
}

func (cfg *ActionConfigString) UnmarshalJSON(data []byte) error {
	s := ActionConfigString(data)
	*cfg = s
	return nil
}

func (p Policy) MarshalJSON() (string, error) {
	return marshalJSON(p)
}

func (p Rule) MarshalJSON() (string, error) {
	return marshalJSON(p)
}

func (p Action) MarshalJSON() (string, error) {
	return marshalJSON(p)
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

func (p Policy) MarshalYAML() (string, error) {
	return marshalJSON(p)
}

func (p Rule) MarshalYAML() (string, error) {
	return marshalJSON(p)
}

func (p Action) MarshalYAML() (string, error) {
	return marshalJSON(p)
}

func marshalYAML(o any) (string, error) {
	bytes, err := yaml.Marshal(o)

	if err != nil {
		return "", err
	}

	return string(bytes), nil
}
