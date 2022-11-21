package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestHTTPHeaders(t *testing.T) {
	cases := testCases[httpOptions, proto.HTTPEndpoint]{
		{
			name: "absent",
			opts: HTTPEndpoint(),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				req := opts.RequestHeaders
				resp := opts.RequestHeaders

				require.Nil(t, req)
				require.Nil(t, resp)
			},
		},
		{
			name: "simple request",
			opts: HTTPEndpoint(
				WithRequestHeader("foo", "bar baz"),
				WithRemoveRequestHeader("baz"),
			),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				req := opts.RequestHeaders
				resp := opts.ResponseHeaders

				require.NotNil(t, req)
				require.Nil(t, resp)

				require.Contains(t, req.Add, "foo:bar baz")
				require.Contains(t, req.Remove, "baz")
			},
		},
		{
			name: "multiple request",
			opts: HTTPEndpoint(
				WithRequestHeader("foo", "bar"),
				WithRequestHeader("foo", "baz"),
				WithRequestHeader("spam", "eggs"),
				WithRemoveRequestHeader("qas"),
				WithRemoveRequestHeader("wex"),
			),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				req := opts.RequestHeaders
				resp := opts.ResponseHeaders

				require.NotNil(t, req)
				require.Nil(t, resp)

				require.Contains(t, req.Add, "foo:bar;baz")
				require.Contains(t, req.Add, "spam:eggs")
				require.Contains(t, req.Remove, "qas")
				require.Contains(t, req.Remove, "wex")
			},
		},
		{
			name: "simple response",
			opts: HTTPEndpoint(
				WithResponseHeader("foo", "bar baz"),
				WithRemoveResponseHeader("baz"),
			),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				req := opts.RequestHeaders
				resp := opts.ResponseHeaders

				require.Nil(t, req)
				require.NotNil(t, resp)

				require.Contains(t, resp.Add, "foo:bar baz")
				require.Contains(t, resp.Remove, "baz")
			},
		},
		{
			name: "multiple response",
			opts: HTTPEndpoint(
				WithResponseHeader("foo", "bar baz"),
				WithResponseHeader("spam", "eggs"),
				WithRemoveResponseHeader("qas"),
				WithRemoveResponseHeader("wex"),
			),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				req := opts.RequestHeaders
				resp := opts.ResponseHeaders

				require.Nil(t, req)
				require.NotNil(t, resp)

				require.Contains(t, resp.Add, "foo:bar baz")
				require.Contains(t, resp.Add, "spam:eggs")
				require.Contains(t, resp.Remove, "qas")
				require.Contains(t, resp.Remove, "wex")
			},
		},
		{
			name: "multiple request response",
			opts: HTTPEndpoint(
				WithRequestHeader("foo", "bar baz"),
				WithRequestHeader("spam", "eggs"),
				WithRemoveRequestHeader("qas"),
				WithRemoveRequestHeader("wex"),
				WithResponseHeader("foo", "bar baz"),
				WithResponseHeader("spam", "eggs"),
				WithRemoveResponseHeader("qas"),
				WithRemoveResponseHeader("wex"),
			),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				req := opts.ResponseHeaders
				resp := opts.ResponseHeaders

				require.NotNil(t, req)
				require.NotNil(t, resp)

				require.Contains(t, req.Add, "foo:bar baz")
				require.Contains(t, req.Add, "spam:eggs")
				require.Contains(t, req.Remove, "qas")
				require.Contains(t, req.Remove, "wex")
				require.Contains(t, resp.Add, "foo:bar baz")
				require.Contains(t, resp.Add, "spam:eggs")
				require.Contains(t, resp.Remove, "wex")
				require.Contains(t, resp.Remove, "qas")
			},
		},
	}

	cases.runAll(t)
}
