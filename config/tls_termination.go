package config

// WithTermination sets the key and certificate in PEM format for TLS termination at the ngrok
// edge.
func WithTermination(certPEM, keyPEM []byte) TLSEndpointOption {
	return tlsOptionFunc(func(cfg *tlsOptions) {
		cfg.CertPEM = certPEM
		cfg.KeyPEM = keyPEM
	})
}

// WithManagedTermination sets the TLS termination at edge for ngrok managed domains.
func WithManagedTermination() TLSEndpointOption {
	return tlsOptionFunc(func(cfg *tlsOptions) {
		cfg.CertPEM = []byte{}
		cfg.KeyPEM = []byte{}
	})
}
