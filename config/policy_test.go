package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/pb"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
	"golang.ngrok.com/ngrok/trafficpolicy"
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
					trafficpolicy.Policy{
						Inbound: []trafficpolicy.Rule{
							{
								Name:        "denyPUT",
								Expressions: []string{"req.Method == 'PUT'"},
								Actions: []trafficpolicy.Action{
									{Type: "deny"},
								},
							},
							{
								Name:        "logFooHeader",
								Expressions: []string{"'foo' in req.Headers"},
								Actions: []trafficpolicy.Action{
									{
										Type:   "log",
										Config: `{"metadata": {"key": "val"}}`,
									},
								},
							},
						},
						Outbound: []trafficpolicy.Rule{
							{
								Name: "InternalErrorWhenFailed",
								Expressions: []string{
									"res.StatusCode <= '0'",
									"res.StatusCode >= '300'",
								},
								Actions: []trafficpolicy.Action{
									{
										Type:   "custom-response",
										Config: `"status_code": 500}`,
									},
								},
							},
						},
					},
				),
			),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getPolicies(opts)
				require.NotNil(t, actual)
				require.Len(t, actual.Inbound, 2)
				require.Equal(t, "denyPUT", actual.Inbound[0].Name)
				require.Equal(t, actual.Inbound[0].Actions, []*pb.MiddlewareConfiguration_PolicyAction{{Type: "deny"}})
				require.Len(t, actual.Outbound, 1)
				require.Len(t, actual.Outbound[0].Expressions, 2)
			},
		},
		{
			name: "with policy string",
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
				require.NotNil(t, actual)
				require.Len(t, actual.Inbound, 2)
				require.Equal(t, "denyPut", actual.Inbound[0].Name)
				require.Equal(t, []*pb.MiddlewareConfiguration_PolicyAction{{Type: "deny"}}, actual.Inbound[0].Actions)
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
