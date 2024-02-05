package trafficpolicy

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
							Config: `{"metadata":{"key":"val"}}`,
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
							Config: `{"status_code":500}`,
						},
					},
				},
			},
		}

		json, err := cfg.MarshalJSON()
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
					Config: `{"status_code": 401}`,
				},
			},
		}

		result, err := policy.MarshalJSON()
		require.NoError(t, err)
		require.JSONEq(t, expected, result)
	})

	t.Run("convert action to json", func(t *testing.T) {
		expected := `{"type":"deny","config":{"status_code":401}}`
		action := Action{

			Type:   "deny",
			Config: `{"status_code": 401}`,
		}

		result, err := action.MarshalJSON()
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
							Config: `{"metadata":{"key":"val"}}`,
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
							Config: `{"status_code":500}`,
						},
					},
				},
			},
		}

		yaml, err := cfg.MarshalYAML()
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
					Config: `{"status_code": 401}`,
				},
			},
		}

		result, err := policy.MarshalYAML()
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
			Config: `{"status_code": 401}`,
		}

		result, err := action.MarshalYAML()
		require.NoError(t, err)
		require.YAMLEq(t, expected, result)
	})
}
