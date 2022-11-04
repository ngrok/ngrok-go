package config

import (
	"net/http"
	"testing"

	_ "embed"
)

type nopHandler struct{}

func (f nopHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {}

func testHTTPServer[T tunnelConfigPrivate, OT any](t *testing.T,
	makeOpts func(...OT) Tunnel,
) {
	optsFunc := func(opts ...any) Tunnel {
		return makeOpts(assertSlice[OT](opts)...)
	}

	handler := nopHandler{}
	srv := &http.Server{}

	cases := testCases[T, any]{
		{
			name:             "absent",
			opts:             optsFunc(),
			expectHTTPServer: serverPtr(nil),
		},
		{
			name:             "with server",
			opts:             optsFunc(WithHTTPServer(srv)),
			expectHTTPServer: serverPtr(srv),
		},
		{
			name:              "with handler",
			opts:              optsFunc(WithHTTPHandler(handler)),
			expectHTTPHandler: handlerPtr(handler),
		},
	}

	cases.runAll(t)
}

func TestHTTPServer(t *testing.T) {
	testHTTPServer[httpOptions](t, HTTPEndpoint)
	testHTTPServer[tlsOptions](t, TLSEndpoint)
	testHTTPServer[tcpOptions](t, TCPEndpoint)
	testHTTPServer[labeledOptions](t, LabeledTunnel)
}
