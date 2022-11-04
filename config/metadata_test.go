package config

import (
	"testing"
)

func testMetadata[T tunnelConfigPrivate, OT any](t *testing.T,
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
				Metadata: stringPtr(""),
			},
		},
		{
			name: "with metadata",
			opts: optsFunc(WithMetadata("Hello, world!")),
			expectExtra: &matchBindExtra{
				Metadata: stringPtr("Hello, world!"),
			},
		},
	}

	cases.runAll(t)
}

func TestMetadata(t *testing.T) {
	testMetadata[httpOptions](t, HTTPEndpoint)
	testMetadata[tlsOptions](t, TLSEndpoint)
	testMetadata[tcpOptions](t, TCPEndpoint)
	testMetadata[labeledOptions](t, LabeledTunnel)
}
