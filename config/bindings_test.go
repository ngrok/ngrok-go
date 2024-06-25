package config

import (
	"testing"
)

func testBindings[T tunnelConfigPrivate, OT any](t *testing.T,
	makeOpts func(...OT) Tunnel,
) {
	optsFunc := func(opts ...any) Tunnel {
		return makeOpts(assertSlice[OT](opts)...)
	}

	cases := testCases[T, any]{
		{
			name: "absent",
			opts: optsFunc(),
			expectExtra: &matchBindExtra{
				Bindings: ptr([]string{}),
			},
		},
		{
			name: "with bindings",
			opts: optsFunc(WithBindings("public")),
			expectExtra: &matchBindExtra{
				Bindings: ptr([]string{"public"}),
			},
		},
		{
			name: "with bindings with spread op",
			opts: optsFunc(WithBindings([]string{"public"}...)),
			expectExtra: &matchBindExtra{
				Bindings: ptr([]string{"public"}),
			},
		},
	}

	cases.runAll(t)
}

func TestBindings(t *testing.T) {
	testBindings[*httpOptions](t, HTTPEndpoint)
	testBindings[*tlsOptions](t, TLSEndpoint)
	testBindings[*tcpOptions](t, TCPEndpoint)
}
