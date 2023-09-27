package config

import (
	"testing"
)

func testForwardsTo[T tunnelConfigPrivate, OT any](t *testing.T,
	makeOpts func(...OT) Tunnel,
) {
	optsFunc := func(opts ...any) Tunnel {
		return makeOpts(assertSlice[OT](opts)...)
	}

	cases := testCases[T, any]{
		{
			name:             "absent",
			opts:             optsFunc(),
			expectForwardsTo: ptr(defaultForwardsTo()),
		},
		{
			name:             "with forwardsTo",
			opts:             optsFunc(WithForwardsTo("localhost:8080")),
			expectForwardsTo: ptr("localhost:8080"),
		},
	}

	cases.runAll(t)
}

func TestForwardsTo(t *testing.T) {
	testForwardsTo[httpOptions](t, HTTPEndpoint)
	testForwardsTo[tlsOptions](t, TLSEndpoint)
	testForwardsTo[tcpOptions](t, TCPEndpoint)
	testForwardsTo[labeledOptions](t, LabeledTunnel)
}
