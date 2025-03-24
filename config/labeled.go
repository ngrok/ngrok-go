package config

import (
	"net/http"
	"net/url"

	"golang.ngrok.com/ngrok/internal/tunnel/proto"
)

type LabeledTunnelOption interface {
	ApplyLabeled(cfg *labeledOptions)
}

type labeledOptionFunc func(cfg *labeledOptions)

func (of labeledOptionFunc) ApplyLabeled(cfg *labeledOptions) {
	of(cfg)
}

// LabeledTunnel constructs a new set options for a labeled Edge.
//
// https://ngrok.com/docs/network-edge/edges/#tunnel-group
func LabeledTunnel(opts ...LabeledTunnelOption) Tunnel {
	cfg := labeledOptions{}
	for _, opt := range opts {
		opt.ApplyLabeled(&cfg)
	}
	return &cfg
}

// Options for labeled tunnels.
type labeledOptions struct {
	// Common tunnel configuration options.
	commonOpts

	// A map of label, value pairs for this tunnel.
	labels map[string]string

	// An HTTP Server to run traffic on
	// Deprecated: Pass HTTP server refs via session.ListenAndServeHTTP instead.
	httpServer *http.Server
}

// WithLabel adds a label to this tunnel's set of label, value pairs.
func WithLabel(label, value string) LabeledTunnelOption {
	return labeledOptionFunc(func(cfg *labeledOptions) {
		if cfg.labels == nil {
			cfg.labels = map[string]string{}
		}

		cfg.labels[label] = value
	})
}

func (cfg labeledOptions) ForwardsProto() string {
	return cfg.commonOpts.ForwardsProto
}

func (cfg labeledOptions) ForwardsTo() string {
	return cfg.commonOpts.getForwardsTo()
}

func (cfg *labeledOptions) WithForwardsTo(url *url.URL) {
	cfg.commonOpts.ForwardsTo = url.Host
}

func (cfg labeledOptions) Extra() proto.BindExtra {
	return proto.BindExtra{
		Name:        cfg.Name,
		Metadata:    cfg.Metadata,
		Description: cfg.Description,
	}
}

func (cfg labeledOptions) Proto() string {
	return ""
}

func (cfg labeledOptions) Opts() any {
	return nil
}

func (cfg labeledOptions) Labels() map[string]string {
	return cfg.labels
}

func (cfg labeledOptions) HTTPServer() *http.Server {
	return cfg.httpServer
}

// compile-time check that we're implementing the proper interfaces.
var _ interface {
	tunnelConfigPrivate
	Tunnel
} = (*labeledOptions)(nil)
