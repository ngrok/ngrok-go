package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/pb"
	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func testPolicies[T tunnelConfigPrivate, O any, OT any](t *testing.T,
	makeOpts func(...OT) Tunnel,
	getPolicies func(*O) *pb.MiddlewareConfiguration_Policies,
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
			name: "with policies",
			opts: optsFunc(
				WithPolicies(
					WithInboundPolicy(
						WithName("deny put requests"),
						WithExpression("req.Method == 'PUT'"),
						WithAction(WithType("deny"))),
					WithInboundPolicy(
						WithName("log 'foo' header"),
						WithExpression("'foo' in req.Headers"),
						WithAction(
							WithType("log"),
							WithConfig("{\"key\":\"val\"}"))),
					WithOutboundPolicy(
						WithName("return 500 when response not success"),
						WithExpression("res.StatusCode <= 0"),
						WithExpression("&& res.StatusCode >= 300"),
						WithAction(
							WithType("custom_response"),
							WithConfig(`{"status_code":500}`))))),
			expectOpts: func(t *testing.T, opts *O) {
				actual := getPolicies(opts)
				require.NotNil(t, actual)
				require.Len(t, actual.Inbound, 2)
				require.Equal(t, "deny put requests", actual.Inbound[0].Name)
				require.Equal(t, actual.Inbound[0].Actions, []*pb.MiddlewareConfiguration_Action{{Type: "deny"}})
				require.Len(t, actual.Outbound, 1)
				require.Len(t, actual.Outbound[0].Expressions, 2)
			},
		},
	}

	cases.runAll(t)
}

func TestPolicies(t *testing.T) {
	testPolicies[*httpOptions](t, HTTPEndpoint,
		func(h *proto.HTTPEndpoint) *pb.MiddlewareConfiguration_Policies {
			return h.Policies
		})
	testPolicies[*tcpOptions](t, TCPEndpoint,
		func(h *proto.TCPEndpoint) *pb.MiddlewareConfiguration_Policies {
			return h.Policies
		})
	testPolicies[*tlsOptions](t, TLSEndpoint,
		func(h *proto.TLSEndpoint) *pb.MiddlewareConfiguration_Policies {
			return h.Policies
		})
}
