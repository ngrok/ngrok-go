package config

type TLSTerminationLocation int

const (
	// Terminate TLS at the ngrok edge. The backend will receive a plaintext
	// stream.
	TLSAtEdge TLSTerminationLocation = iota
	// Terminate TLS in the ngrok library. The library will receive the
	// handshake and perform TLS termination, and the backend will receive the
	// plaintext stream.
	// TODO: export this once implemented
	tlsAtLibrary
)

type tlsTermination struct {
	location TLSTerminationLocation
	key      []byte
	cert     []byte
}

func (tt tlsTermination) ApplyTLS(cfg *tlsOptions) {
	switch tt.location {
	case tlsAtLibrary:
		cfg.KeyPEM = nil
		cfg.CertPEM = nil
		// TODO: implement this in the tunnel `Accept` call.
		panic("automatic tls termination in-app is not yet supported")
	case TLSAtEdge:
		cfg.terminateAtEdge = true
		cfg.KeyPEM = tt.key
		cfg.CertPEM = tt.cert
		return
	}
}

type TLSTerminationOption func(tt *tlsTermination)

// WithTLSTermination arranges for incoming TLS connections to be automatically terminated.
// The backend will then receive plaintext streams, rather than raw TLS connections.
// Defaults to terminating TLS at the ngrok edge with an automatically-provisioned keypair.
//
// https://ngrok.com/docs/tls/tls-termination/
func WithTLSTermination(opts ...TLSTerminationOption) TLSEndpointOption {
	tt := tlsTermination{
		location: TLSAtEdge,
		key:      []byte{},
		cert:     []byte{},
	}
	for _, opt := range opts {
		opt(&tt)
	}
	return tt
}

// WithTermination sets the key and certificate in PEM format for TLS termination at the ngrok
// edge.
//
// Deprecated: Use WithCustomEdgeTermination instead.
func WithTermination(certPEM, keyPEM []byte) TLSEndpointOption {
	return tlsOptionFunc(func(cfg *tlsOptions) {
		cfg.terminateAtEdge = true
		cfg.CertPEM = certPEM
		cfg.KeyPEM = keyPEM
	})
}

// WithTLSTerminationAt determines where TLS termination should occur.
// Currently, only `TLSAtEdge` is supported.
func WithTLSTerminationAt(location TLSTerminationLocation) TLSTerminationOption {
	return TLSTerminationOption(func(cfg *tlsTermination) {
		cfg.location = location
	})
}

// WithTLSTerminationKeyPair sets a custom key and certificate in PEM format for
// TLS termination.
// If terminating at the ngrok edge, this uploads the private key and
// certificate to the ngrok servers.
func WithTLSTerminationKeyPair(certPEM, keyPEM []byte) TLSTerminationOption {
	return TLSTerminationOption(func(cfg *tlsTermination) {
		cfg.cert = certPEM
		cfg.key = keyPEM
	})
}
