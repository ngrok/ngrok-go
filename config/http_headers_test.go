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

				require.Equal(t, []string{"Foo:bar baz"}, req.Add)
				require.Equal(t, []string{"Baz"}, req.Remove)
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

				require.ElementsMatch(t, []string{"Foo:bar;baz", "Spam:eggs"}, req.Add)
				require.ElementsMatch(t, []string{"Qas", "Wex"}, req.Remove)
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

				require.Equal(t, []string{"Foo:bar baz"}, resp.Add)
				require.Equal(t, []string{"Baz"}, resp.Remove)
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

				require.ElementsMatch(t, []string{"Foo:bar baz", "Spam:eggs"}, resp.Add)
				require.ElementsMatch(t, []string{"Qas", "Wex"}, resp.Remove)
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

				require.ElementsMatch(t, []string{"Spam:eggs", "Foo:bar baz"}, resp.Add)
				require.ElementsMatch(t, []string{"Qas", "Wex"}, resp.Remove)
			},
		},
	}

	cases.runAll(t)
}
