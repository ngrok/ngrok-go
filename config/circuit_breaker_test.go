package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

func TestCircuitBreaker(t *testing.T) {
	cases := testCases[httpOptions, proto.HTTPEndpoint]{
		{
			name: "absent",
			opts: HTTPEndpoint(),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				require.Nil(t, opts.CircuitBreaker)
			},
		},
		{
			name: "breakered",
			opts: HTTPEndpoint(WithCircuitBreaker(0.5)),
			expectOpts: func(t *testing.T, opts *proto.HTTPEndpoint) {
				require.NotNil(t, opts.CircuitBreaker)
				require.Equal(t, opts.CircuitBreaker.ErrorThreshold, 0.5)
			},
		},
	}

	cases.runAll(t)
}
