package policy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPolicyToJSON(t *testing.T) {
	t.Run("Convert whole policy to json", func(t *testing.T) {
		expected := `
		{
			"inbound": [
				{
					"name":"denyPUT",
					"expressions":["req.Method == 'PUT'"],
					"actions":[
						{
							"type":"deny"
						}
					]
				},
				{
					"name":"logFooHeader",
					"expressions":["'foo' in req.Headers"],
					"actions":[
						{
							"type":"log",
							"config":{
								"metadata":{
									"key":"val"
								}
							}
						}
					]
				}
			],
			"outbound": [
				{
					"name":"InternalErrorWhenFailed",
					"expressions":["res.StatusCode <= '0'", "res.StatusCode >= '300'"],
					"actions":[
						{
							"type":"custom-response",
							"config":{
								"status_code":500
							}
						}
					]
				}
			]
		}`
		cfg := Policy{
			Inbound: []Rule{
				{
					Name:        "denyPUT",
					Expressions: []string{"req.Method == 'PUT'"},
					Actions: []Action{
						{Type: "deny"},
					},
				},
				{
					Name:        "logFooHeader",
					Expressions: []string{"'foo' in req.Headers"},
					Actions: []Action{
						{
							Type:   "log",
							Config: map[string]any{"metadata": map[string]any{"key": "val"}},
						},
					},
				},
			},
			Outbound: []Rule{
				{
					Name: "InternalErrorWhenFailed",
					Expressions: []string{
						"res.StatusCode <= '0'",
						"res.StatusCode >= '300'",
					},
					Actions: []Action{
						{
							Type:   "custom-response",
							Config: map[string]any{"status_code": 500},
						},
					},
				},
			},
		}

		json, err := cfg.JSON()
		require.NoError(t, err)
		require.JSONEq(t, expected, json)
	})

	t.Run("convert policy rule to json", func(t *testing.T) {
		expected := `{"name":"denyPUT","expressions":["req.Method == 'PUT'"],"actions":[{"type":"deny","config":{"status_code":401}}]}`

		policy := Rule{
			Name:        "denyPUT",
			Expressions: []string{"req.Method == 'PUT'"},
			Actions: []Action{
				{
					Type:   "deny",
					Config: map[string]any{"status_code": 401},
				},
			},
		}

		result, err := policy.JSON()
		require.NoError(t, err)
		require.JSONEq(t, expected, result)
	})

	t.Run("convert action to json", func(t *testing.T) {
		expected := `{"type":"deny","config":{"status_code":401}}`
		action := Action{

			Type:   "deny",
			Config: map[string]any{"status_code": 401},
		}

		result, err := action.JSON()
		require.NoError(t, err)
		require.JSONEq(t, expected, result)
	})
}

func TestPolicyToYAML(t *testing.T) {
	t.Run("Convert whole policy to yaml", func(t *testing.T) {
		expected := `
            inbound:
              - name: "denyPUT"
                expressions: ["req.Method == 'PUT'"]
                actions:
                  - type: "deny"
              - name: "logFooHeader"
                expressions: ["'foo' in req.Headers"]
                actions:
                  - type: "log"
                    config:
                        metadata:
                            key: "val"
            outbound:
              - name: "InternalErrorWhenFailed"
                expressions:
                  - "res.StatusCode <= '0'"
                  - "res.StatusCode >= '300'"
                actions:
                  - type: "custom-response"
                    config:
                        status_code: 500`
		cfg := Policy{
			Inbound: []Rule{
				{
					Name:        "denyPUT",
					Expressions: []string{"req.Method == 'PUT'"},
					Actions: []Action{
						{Type: "deny"},
					},
				},
				{
					Name:        "logFooHeader",
					Expressions: []string{"'foo' in req.Headers"},
					Actions: []Action{
						{
							Type:   "log",
							Config: map[string]any{"metadata": map[string]any{"key": "val"}},
						},
					},
				},
			},
			Outbound: []Rule{
				{
					Name: "InternalErrorWhenFailed",
					Expressions: []string{
						"res.StatusCode <= '0'",
						"res.StatusCode >= '300'",
					},
					Actions: []Action{
						{
							Type:   "custom-response",
							Config: map[string]any{"status_code": 500},
						},
					},
				},
			},
		}

		yaml, err := cfg.YAML()
		require.NoError(t, err)
		require.YAMLEq(t, expected, yaml)
	})

	t.Run("convert policy rule to json", func(t *testing.T) {
		expected := `
            name: "denyPUT"
            expressions: ["req.Method == 'PUT'"]
            actions:
              - type: "deny"
                config:
                    status_code: 401`

		policy := Rule{
			Name:        "denyPUT",
			Expressions: []string{"req.Method == 'PUT'"},
			Actions: []Action{
				{
					Type:   "deny",
					Config: map[string]any{"status_code": 401},
				},
			},
		}

		result, err := policy.YAML()
		require.NoError(t, err)
		require.YAMLEq(t, expected, result)
	})

	t.Run("convert action to json", func(t *testing.T) {
		expected := `
            type: "deny"
            config:
                status_code: 401`
		action := Action{

			Type:   "deny",
			Config: map[string]any{"status_code": 401},
		}

		result, err := action.YAML()
		require.NoError(t, err)
		require.YAMLEq(t, expected, result)
	})
}

func TestFromString(t *testing.T) {
	t.Run("rule from json", func(t *testing.T) {
		input := `{"name":"denyPUT","expressions":["req.Method == 'PUT'"],"actions":[{"type":"deny","config":{"status_code":401}}]}`
		expected := Rule{
			Name:        "denyPUT",
			Expressions: []string{"req.Method == 'PUT'"},
			Actions: []Action{
				{
					Type:   "deny",
					Config: map[string]any{"status_code": 401},
				},
			},
		}

		result, err := NewRuleFromString(input)

		require.NoError(t, err)
		require.Equal(t, expected, result)
	})

	t.Run("new rule from yaml", func(t *testing.T) {
		input := `
            name: "denyPUT"
            expressions: ["req.Method == 'PUT'"]
            actions:
              - type: "deny"
                config:
                    status_code: 401`

		expected := Rule{
			Name:        "denyPUT",
			Expressions: []string{"req.Method == 'PUT'"},
			Actions: []Action{
				{
					Type:   "deny",
					Config: map[string]any{"status_code": 401},
				},
			},
		}

		result, err := NewRuleFromString(input)

		require.NoError(t, err)
		require.Equal(t, expected, result)
	})

	t.Run("convert action to json", func(t *testing.T) {
		input := `{"type":"deny","config":{"status_code":401}}`
		expected := Action{

			Type:   "deny",
			Config: map[string]any{"status_code": 401},
		}

		result, err := NewActionFromString(input)

		require.NoError(t, err)
		require.Equal(t, expected, result)
	})

	t.Run("action from yaml", func(t *testing.T) {
		input := `
            type: "deny"
            config:
                status_code: 401`
		expected := Action{

			Type:   "deny",
			Config: map[string]any{"status_code": 401},
		}

		result, err := NewActionFromString(input)

		require.NoError(t, err)
		require.Equal(t, expected, result)
	})

	t.Run("must action to json", func(t *testing.T) {
		input := `{"type":"deny","config":{"status_code":401}}`
		expected := Action{

			Type:   "deny",
			Config: map[string]any{"status_code": 401},
		}

		result := MustActionFromString(input)

		require.Equal(t, expected, result)
	})

	t.Run("must action from yaml", func(t *testing.T) {
		input := `
            type: "deny"
            config:
                status_code: 401`
		expected := Action{

			Type:   "deny",
			Config: map[string]any{"status_code": 401},
		}

		result := MustActionFromString(input)

		require.Equal(t, expected, result)
	})

	t.Run("must action from invalid", func(t *testing.T) {
		input := `invalid: val`

		require.Panics(t, func() { MustActionFromString(input) })
	})
}
