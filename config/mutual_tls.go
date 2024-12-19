package config

import (
	"crypto/x509"
	"encoding/pem"

	"golang.ngrok.com/ngrok/internal/mw"
)

type mutualTLSEndpointOption []*x509.Certificate

// WithMutualTLSCA adds a list of [x509.Certificate]'s to use for mutual TLS
// authentication.
// These will be used to authenticate client certificates for requests at the
// ngrok edge.
//
// https://ngrok.com/docs/http/mutual-tls/
func WithMutualTLSCA(certs ...*x509.Certificate) interface {
	HTTPEndpointOption
	TLSEndpointOption
} {
	return mutualTLSEndpointOption(certs)
}

func (opt mutualTLSEndpointOption) ApplyHTTP(opts *httpOptions) {
	opts.MutualTLSCA = append(opts.MutualTLSCA, opt...)
}

func (opt mutualTLSEndpointOption) ApplyTLS(opts *tlsOptions) {
	opts.MutualTLSCA = append(opts.MutualTLSCA, opt...)
}

func (cfg mutualTLSEndpointOption) toProtoConfig() *mw.MiddlewareConfiguration_MutualTLS {
	if cfg == nil {
		return nil
	}
	opts := &mw.MiddlewareConfiguration_MutualTLS{}
	for _, cert := range cfg {
		opts.MutualTlsCa = append(opts.MutualTlsCa, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})...)
	}
	return opts
}
