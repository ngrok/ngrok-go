package config

import (
	"testing"
)

func testBinding[T tunnelConfigPrivate, OT any](t *testing.T,
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
				Binding: ptr(""),
			},
		},
		{
			name: "with bindings",
			opts: optsFunc(WithBinding("public")),
			expectExtra: &matchBindExtra{
				Binding: ptr("public"),
			},
		},
		{
			name: "with bindings",
			opts: optsFunc(WithBinding("internal")),
			expectExtra: &matchBindExtra{
				Binding: ptr("internal"),
			},
		},
	}

	cases.runAll(t)
}

func TestBinding(t *testing.T) {
	testBinding[*httpOptions](t, HTTPEndpoint)
	testBinding[*tlsOptions](t, TLSEndpoint)
	testBinding[*tcpOptions](t, TCPEndpoint)
}
