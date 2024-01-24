package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/pb"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func testPolicy[T tunnelConfigPrivate, O any, OT any](t *testing.T,
	makeOpts func(...OT) Tunnel,
	getPolicies func(*O) *pb.MiddlewareConfiguration_Policy,
) {
	optsFunc := func(opts ...any) Tunnel {
		return makeOpts(assertSlice[OT](opts)...)
	}
	cases := testCases[T, O]{
		{
			name: "absent",
			opts: optsFunc(),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getPolicies(opts)
				require.Nil(t, actual)
			},
		},
		{
			name: "with policy",
			opts: optsFunc(
				WithPolicy(
					WithInboundRules(
						WithPolicyRule(
							WithPolicyName("deny put requests"),
							WithPolicyExpression("req.Method == 'PUT'"),
							WithPolicyAction(WithPolicyActionType("deny")),
						),
						WithPolicyRule(
							WithPolicyName("log 'foo' header"),
							WithPolicyExpression("'foo' in req.Headers"),
							WithPolicyAction(
								WithPolicyActionType("log"),
								WithPolicyActionConfig(map[string]any{
									"metadata": map[string]any{
										"key": "val",
									},
								}),
							),
						),
					),
					WithOutboundRules(
						WithPolicyRule(
							WithPolicyName("return 500 when response not success"),
							WithPolicyExpression("res.StatusCode <= 0"),
							WithPolicyExpression("res.StatusCode >= 300"),
							WithPolicyAction(
								WithPolicyActionType("custom-response"),
								WithPolicyActionConfig(map[string]any{
									"status_code": 500,
								}),
							),
						),
					),
				),
			),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getPolicies(opts)
				require.NotNil(t, actual)
				require.Len(t, actual.Inbound, 2)
				require.Equal(t, "deny put requests", actual.Inbound[0].Name)
				require.Equal(t, actual.Inbound[0].Actions, []*pb.MiddlewareConfiguration_PolicyAction{{Type: "deny"}})
				require.Len(t, actual.Outbound, 1)
				require.Len(t, actual.Outbound[0].Expressions, 2)
			},
		},
		{
			name: "with policy config",
			opts: optsFunc(
				WithPolicyConfig(`
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
				require.NotNil(t, actual)
				require.Len(t, actual.Inbound, 2)
				require.Equal(t, "denyPut", actual.Inbound[0].Name)
				require.Equal(t, actual.Inbound[0].Actions, []*pb.MiddlewareConfiguration_PolicyAction{{Type: "deny"}})
				require.Len(t, actual.Outbound, 1)
				require.Len(t, actual.Outbound[0].Expressions, 2)
			},
		},
	}

	cases.runAll(t)
}

func TestPolicy(t *testing.T) {
	testPolicy[*httpOptions](t, HTTPEndpoint,
		func(h *proto.HTTPEndpoint) *pb.MiddlewareConfiguration_Policy {
			return h.Policy
		})
	testPolicy[*tcpOptions](t, TCPEndpoint,
		func(h *proto.TCPEndpoint) *pb.MiddlewareConfiguration_Policy {
			return h.Policy
		})
	testPolicy[*tlsOptions](t, TLSEndpoint,
		func(h *proto.TLSEndpoint) *pb.MiddlewareConfiguration_Policy {
			return h.Policy
		})
}

func TestPolicyToJSON(t *testing.T) {
	t.Run("Convert whole policy to json", func(t *testing.T) {
		cfg := WithPolicy(
			WithInboundRules(
				WithPolicyRule(
					WithPolicyName("deny put requests"),
					WithPolicyExpression("req.Method == 'PUT'"),
					WithPolicyAction(
						WithPolicyActionType("deny"))),
				WithPolicyRule(
					WithPolicyName("log 'foo' header"),
					WithPolicyExpression("'foo' in req.Headers"),
					WithPolicyAction(
						WithPolicyActionType("log"),
						WithPolicyActionConfig(map[string]any{"metadata": map[string]any{"key": "val"}})))),
			WithOutboundRules(
				WithPolicyRule(
					WithPolicyName("return 500 when response not success"),
					WithPolicyExpression("res.StatusCode <= 0"),
					WithPolicyExpression("res.StatusCode >= 300"),
					WithPolicyAction(
						WithPolicyActionType("csustom-response"),
						WithPolicyActionConfig(map[string]any{"status_code": 500})))))

		json := cfg.ToJSON()

		result := WithPolicyConfig(json)

		require.Equal(t, cfg, result)
	})

	t.Run("convert policy rule to json", func(t *testing.T) {
		expected := `{"name":"denyPut","expressions":["req.Method == 'PUT'"],"actions":[{"type":"deny","config":{"status_code":401}}]}`

		policy := WithPolicyRule(
			WithPolicyName("denyPut"),
			WithPolicyExpression("req.Method == 'PUT'"),
			WithPolicyAction(
				WithPolicyActionType("deny"),
				WithPolicyActionConfig(map[string]any{
					"status_code": 401,
				}),
			),
		)

		result := policy.ToJSON()

		require.Equal(t, expected, result)
	})

	t.Run("convert action to json", func(t *testing.T) {
		expected := `{"type":"deny","config":{"status_code":401}}`
		action := WithPolicyAction(
			WithPolicyActionType("deny"),
			WithPolicyActionConfig(map[string]any{
				"status_code": 401,
			}),
		)

		result := action.ToJSON()

		require.Equal(t, expected, result)
	})
}
