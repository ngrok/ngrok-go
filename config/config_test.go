package config

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

// Helper to assert a whole slice to a different type.
// See cidr_restrictions_test.go for an example of its application.
func assertSlice[T any](opts []any) []T {
	out := []T{}
	for _, opt := range opts {
		out = append(out, opt.(T))
	}
	return out
}

func handlerPtr(h http.Handler) *http.Handler {
	return &h
}

func serverPtr(srv *http.Server) **http.Server {
	return &srv
}

func labelPtr(labels map[string]*string) *map[string]*string {
	return &labels
}

func stringPtr(s string) *string {
	return &s
}

type matchBindExtra struct {
	Token       *string
	IPPolicyRef *string
	Metadata    *string
}

func (m matchBindExtra) RequireMatches(t *testing.T, actual proto.BindExtra) {
	if m.Token != nil {
		require.Equal(t, *m.Token, actual.Token)
	}
	if m.IPPolicyRef != nil {
		require.Equal(t, *m.IPPolicyRef, actual.IPPolicyRef)
	}
	if m.Metadata != nil {
		require.Equal(t, *m.Metadata, actual.Metadata)
	}
}

type testCase[T tunnelConfigPrivate, O any] struct {
	name              string
	opts              Tunnel
	expectForwardsTo  *string
	expectProto       *string
	expectExtra       *matchBindExtra
	expectLabels      *map[string]*string
	expectHTTPServer  **http.Server // TODO: deprecate
	expectHTTPHandler *http.Handler // TODO: deprecate
	expectOpts        func(t *testing.T, opts *O)
	expectNilOpts     bool
}

type testCases[T tunnelConfigPrivate, O any] []testCase[T, O]

func (tc testCase[T, O]) Run(t *testing.T) {
	t.Run(tc.name, func(t *testing.T) {
		actualOpts, ok := tc.opts.(T)
		require.Truef(t, ok, "Tunnel opts should have type %v", reflect.TypeOf(new(T)))
		if tc.expectForwardsTo != nil {
			require.Equal(t, *tc.expectForwardsTo, actualOpts.ForwardsTo())
		}
		if tc.expectProto != nil {
			require.Equal(t, *tc.expectProto, actualOpts.Proto())
		}
		if tc.expectExtra != nil {
			tc.expectExtra.RequireMatches(t, actualOpts.Extra())
		}
		if tc.expectLabels != nil {
			if *tc.expectLabels != nil {
				actual := actualOpts.Labels()
				require.Len(t, actual, len(*tc.expectLabels))
				for k, v := range *tc.expectLabels {
					require.Contains(t, actual, k)
					require.Equal(t, *v, actual[k])
				}
			} else {
				require.Nil(t, actualOpts.Labels())
			}
		}
		if tc.expectNilOpts {
			require.Nil(t, actualOpts.Opts())
		} else if tc.expectOpts != nil {
			opts, ok := actualOpts.Opts().(*O)
			require.Truef(t, ok, "Opts has the type %v", reflect.TypeOf((*O)(nil)))
			tc.expectOpts(t, opts)
		}

		if tc.expectHTTPServer != nil {
			withHTTPServer, ok := tc.opts.(interface {
				HTTPServer() *http.Server
			})
			if *tc.expectHTTPServer != nil {
				require.True(t, ok, "opts should have the HTTPServer method")
				actual := withHTTPServer.HTTPServer()
				require.Equal(t, *tc.expectHTTPServer, actual)
			} else if ok {
				require.Nil(t, withHTTPServer.HTTPServer())
			}
		}

		if tc.expectHTTPHandler != nil {
			withHTTPServer, ok := tc.opts.(interface {
				HTTPServer() *http.Server
			})
			if *tc.expectHTTPHandler != nil {
				require.True(t, ok, "opts should have the HTTPServer method")
				actualServer := withHTTPServer.HTTPServer()
				require.NotNil(t, actualServer)
				actual := actualServer.Handler
				require.Equal(t, *tc.expectHTTPHandler, actual)
			} else if ok {
				actualServer := withHTTPServer.HTTPServer()
				if actualServer != nil {
					require.Nil(t, actualServer.Handler)
				}
			}
		}
	})
}

func (tcs testCases[T, O]) runAll(t *testing.T) {
	for _, tc := range tcs {
		tc.Run(t)
	}
}
