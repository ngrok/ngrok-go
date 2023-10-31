package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestHTTPHeaders(t *testing.T) {
	cases := testCases[*httpOptions, proto.HTTPEndpoint]{
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

				require.Equal(t, []string{"foo:bar baz"}, req.Add)
				require.Equal(t, []string{"baz"}, req.Remove)
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

				require.ElementsMatch(t, []string{"foo:bar;baz", "spam:eggs"}, req.Add)
				require.ElementsMatch(t, []string{"qas", "wex"}, req.Remove)
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

				require.Equal(t, []string{"foo:bar baz"}, resp.Add)
				require.Equal(t, []string{"baz"}, resp.Remove)
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

				require.ElementsMatch(t, []string{"foo:bar baz", "spam:eggs"}, resp.Add)
				require.ElementsMatch(t, []string{"qas", "wex"}, resp.Remove)
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

				require.ElementsMatch(t, []string{"spam:eggs", "foo:bar baz"}, resp.Add)
				require.ElementsMatch(t, []string{"qas", "wex"}, resp.Remove)
			},
		},
	}

	cases.runAll(t)
}
