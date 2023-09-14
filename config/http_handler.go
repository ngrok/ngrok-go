package config

import (
	"net/http"
)

type httpServerOption struct {
	Server *http.Server
}

type options interface {
	HTTPEndpointOption
	TLSEndpointOption
	TCPEndpointOption
	LabeledTunnelOption
}

func (opt *httpServerOption) ApplyHTTP(cfg *httpOptions) {
	cfg.httpServer = opt.Server
}

func (opt *httpServerOption) ApplyTCP(cfg *tcpOptions) {
	cfg.httpServer = opt.Server
}

func (opt *httpServerOption) ApplyTLS(cfg *tlsOptions) {
	cfg.httpServer = opt.Server
}

func (opt *httpServerOption) ApplyLabeled(cfg *labeledOptions) {
	cfg.httpServer = opt.Server
}

// WithHTTPHandler adds the provided credentials to the list of basic
// authentication credentials.
// Deprecated: Use session.ListenAndServeHTTP instead.
func WithHTTPHandler(h http.Handler) options {
	return WithHTTPServer(&http.Server{Handler: h})
}

// WithHTTPServer adds the provided credentials to the list of basic
// authentication credentials.
// Deprecated: Use session.ListenAndServeHTTP instead.
func WithHTTPServer(srv *http.Server) options {
	return &httpServerOption{Server: srv}
}
