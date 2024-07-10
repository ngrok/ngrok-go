package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
	po "golang.ngrok.com/ngrok/policy"
)

func testPolicy[T tunnelConfigPrivate, O any, OT any](t *testing.T,
	makeOpts func(...OT) Tunnel,
	getPolicies func(*O) string,
) {

	// putting yaml string up here as the formatting makes the test
	// cases messy
	yamlPolicy := `---
inbound:
    - name: DenyAll
      actions:
        - type: deny
          config:
            status_code: 446 
`

	optsFunc := func(opts ...any) Tunnel {
		return makeOpts(assertSlice[OT](opts)...)
	}

	cases := testCases[T, O]{
		{
			name: "absent",
			opts: optsFunc(),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getPolicies(opts)
				require.Empty(t, actual)
			},
		},
		{
			name: "with policy",
			opts: optsFunc(
				WithPolicy(
					po.Policy{
						Inbound: []po.Rule{
							{
								Name:        "denyPUT",
								Expressions: []string{"req.Method == 'PUT'"},
								Actions: []po.Action{
									{Type: "deny"},
								},
							},
							{
								Name:        "logFooHeader",
								Expressions: []string{"'foo' in req.Headers"},
								Actions: []po.Action{
									{
										Type:   "log",
										Config: map[string]any{"metadata": map[string]any{"key": "val"}},
									},
								},
							},
						},
						Outbound: []po.Rule{
							{
								Name: "InternalErrorWhenFailed",
								Expressions: []string{
									"res.StatusCode <= '0'",
									"res.StatusCode >= '300'",
								},
								Actions: []po.Action{
									{
										Type:   "custom-response",
										Config: map[string]any{"status_code": 500},
									},
								},
							},
						},
					},
				),
			),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getPolicies(opts)
				require.NotEmpty(t, actual)
				require.Equal(t, actual, "{\"inbound\":[{\"name\":\"denyPUT\",\"expressions\":[\"req.Method == 'PUT'\"],\"actions\":[{\"type\":\"deny\"}]},{\"name\":\"logFooHeader\",\"expressions\":[\"'foo' in req.Headers\"],\"actions\":[{\"type\":\"log\",\"config\":{\"metadata\":{\"key\":\"val\"}}}]}],\"outbound\":[{\"name\":\"InternalErrorWhenFailed\",\"expressions\":[\"res.StatusCode \\u003c= '0'\",\"res.StatusCode \\u003e= '300'\"],\"actions\":[{\"type\":\"custom-response\",\"config\":{\"status_code\":500}}]}]}")
			},
		},
		{
			name: "with valid JSON policy string",
			opts: optsFunc(
				WithPolicyString(`
					{
						"inbound":[
							{
								"name":"denyPut",
								"expressions":["req.Method == 'PUT'"],
								"actions":[{"type":"deny"}]
							},
							{
								"name":"logFooHeader",
								"expressions":["'foo' in req.Headers"],
								"actions":[
									{"type":"log","config":{"metadata":{"key":"val"}}}
								]
							}
						],
						"outbound":[
							{
								"name":"500ForFailures",
								"expressions":["res.StatusCode <= 0", "res.StatusCode >= 300"],
								"actions":[{"type":"custom-response", "config":{"status_code":500}}]
							}
						]
					}`)),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getPolicies(opts)
				require.NotEmpty(t, actual)
				require.Equal(t, actual, `
					{
						"inbound":[
							{
								"name":"denyPut",
								"expressions":["req.Method == 'PUT'"],
								"actions":[{"type":"deny"}]
							},
							{
								"name":"logFooHeader",
								"expressions":["'foo' in req.Headers"],
								"actions":[
									{"type":"log","config":{"metadata":{"key":"val"}}}
								]
							}
						],
						"outbound":[
							{
								"name":"500ForFailures",
								"expressions":["res.StatusCode <= 0", "res.StatusCode >= 300"],
								"actions":[{"type":"custom-response", "config":{"status_code":500}}]
							}
						]
					}`)
			},
		},
		{
			name: "with valid YAML policy string",
			opts: optsFunc(
				WithPolicyString(yamlPolicy)),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getPolicies(opts)
				require.NotEmpty(t, actual)
				require.Equal(t, actual, yamlPolicy)
			},
		},
	}

	cases.runAll(t)
}

func TestPolicy(t *testing.T) {
	testPolicy[*httpOptions](t, HTTPEndpoint,
		func(h *proto.HTTPEndpoint) string {
			return h.TrafficPolicy
		})
	testPolicy[*tcpOptions](t, TCPEndpoint,
		func(h *proto.TCPEndpoint) string {
			return h.TrafficPolicy
		})
	testPolicy[*tlsOptions](t, TLSEndpoint,
		func(h *proto.TLSEndpoint) string {
			return h.TrafficPolicy
		})
}
