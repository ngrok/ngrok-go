package config

import (
	"net/http"
)

type httpServerOption struct {
	Server *http.Server
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
func WithHTTPHandler(h http.Handler) interface {
	HTTPEndpointOption
	TLSEndpointOption
	TCPEndpointOption
	LabeledTunnelOption
} {
	return WithHTTPServer(&http.Server{Handler: h})
}

// WithHTTPServer adds the provided credentials to the list of basic
// authentication credentials.
func WithHTTPServer(srv *http.Server) interface {
	HTTPEndpointOption
	TLSEndpointOption
	TCPEndpointOption
	LabeledTunnelOption
} {
	return &httpServerOption{Server: srv}
}
