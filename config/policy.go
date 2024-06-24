package config

import (
	"encoding/json"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"

	po "golang.ngrok.com/ngrok/policy"
)

type policy po.Policy
type rule po.Rule
type action po.Action
type trafficPolicy string

// WithPolicyString configures this edge with the provided policy configuration
// passed as a json or yaml string and overwrites any previously-set traffic policy
// https://ngrok.com/docs/http/traffic-policy
func WithPolicyString(str string) interface {
	HTTPEndpointOption
	TLSEndpointOption
	TCPEndpointOption
} {
	if !isJsonString(str) && !isYamlStr(str) {
		panic(errors.New("provided string is neither valid JSON nor valid YAML"))
	}
	return trafficPolicy(str)
}

// WithPolicy configures this edge with the given traffic policy and overwrites any
// previously-set traffic policy
// https://ngrok.com/docs/http/traffic-policy/
func WithPolicy(p po.Policy) interface {
	HTTPEndpointOption
	TLSEndpointOption
	TCPEndpointOption
} {
	fmt.Println("WithPolicy has been deprecated. Please use WithPolicyString instead, as WithPolicy will stop working soon.")

	val, err := json.Marshal(p)
	if err != nil {
		panic(errors.New(fmt.Sprintf("failed to parse action configuration due to error: %s", err.Error())))
	}
	fmt.Printf("%s\n", string(val))

	return trafficPolicy(string(val))
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

func isJsonString(jsonStr string) bool {
	var js json.RawMessage
	return json.Unmarshal([]byte(jsonStr), &js) == nil
}

func isYamlStr(yamlStr string) bool {
	var yml map[string]any
	return yaml.Unmarshal([]byte(yamlStr), &yml) == nil
}
