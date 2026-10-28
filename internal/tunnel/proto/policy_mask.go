package proto

import (
	"encoding/json"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// secretMask and secretPolicyFields copy the ngrok agent's rule for printing a
// traffic policy, so a policy logged here masks the same values.
const secretMask = "********"

var secretPolicyFields = []string{"client_secret", "secret", "server_private_key", "private_key"}

// maskOpts returns a copy of bind options with traffic policy secrets masked.
// Opts is shared with the caller, so the copy is a JSON round trip.
func maskOpts(opts any) any {
	if opts == nil {
		return nil
	}
	b, err := json.Marshal(opts)
	if err != nil {
		return "HIDDEN"
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return "HIDDEN"
	}
	if m, ok := v.(map[string]any); ok {
		if policy, ok := m["TrafficPolicy"].(string); ok {
			m["TrafficPolicy"] = maskPolicy(policy)
		} else {
			maskSecretValues(m["TrafficPolicy"])
		}
	}
	return v
}

// maskPolicy returns a YAML or JSON traffic policy as JSON with its secrets
// masked, or HIDDEN when it is not a policy document.
func maskPolicy(policy string) string {
	if policy == "" {
		return ""
	}
	var v any
	if json.Unmarshal([]byte(policy), &v) != nil {
		if yaml.Unmarshal([]byte(policy), &v) != nil {
			return "HIDDEN"
		}
	}
	if _, ok := v.(map[string]any); !ok || hasNonStringKeys(v) {
		return "HIDDEN"
	}
	maskSecretValues(v)
	masked, err := json.Marshal(v)
	if err != nil {
		return "HIDDEN"
	}
	return string(masked)
}

// hasNonStringKeys reports a YAML mapping with a non-string key. The walker
// skips such a map, and newer encoding/json prints it, so the policy is withheld.
func hasNonStringKeys(v any) bool {
	switch v := v.(type) {
	case map[any]any:
		return true
	case map[string]any:
		for _, val := range v {
			if hasNonStringKeys(val) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if hasNonStringKeys(item) {
				return true
			}
		}
	}
	return false
}

func maskCredential(cred string) string {
	user, _, ok := strings.Cut(cred, ":")
	if !ok {
		return secretMask
	}
	return user + ":" + secretMask
}

func maskSecretValues(v any) bool {
	masked := false
	switch v := v.(type) {
	case map[string]any:
		for key, val := range v {
			masked = maskSecretField(v, key, val) || masked
		}
	case []any:
		for _, item := range v {
			masked = maskSecretValues(item) || masked
		}
	}
	return masked
}

func maskSecretField(m map[string]any, key string, val any) bool {
	switch val := val.(type) {
	// An unquoted YAML secret decodes as a number.
	case string, int, int64, uint64, float64:
		if val == "" || !slices.Contains(secretPolicyFields, key) {
			return false
		}
		m[key] = secretMask
		return true
	case []any:
		if key != "credentials" {
			break
		}
		masked := false
		for i, cred := range val {
			if s, ok := cred.(string); ok {
				val[i] = maskCredential(s)
				masked = true
			} else {
				masked = maskSecretValues(cred) || masked
			}
		}
		return masked
	}
	return maskSecretValues(val)
}
