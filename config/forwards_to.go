package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// WithForwardsTo sets the ForwardsTo string for this tunnel.
// This can be veiwed via the API or dashboard.
func WithForwardsTo(meta string) interface {
	HTTPEndpointOption
	LabeledTunnelOption
	TCPEndpointOption
	TLSEndpointOption
} {
	return forwardsToOption(meta)
}

type forwardsToOption string

func (fwd forwardsToOption) ApplyHTTP(cfg *httpOptions) {
	cfg.commonOpts.ForwardsTo = string(fwd)
}

func (fwd forwardsToOption) ApplyTCP(cfg *tcpOptions) {
	cfg.commonOpts.ForwardsTo = string(fwd)
}

func (fwd forwardsToOption) ApplyTLS(cfg *tlsOptions) {
	cfg.commonOpts.ForwardsTo = string(fwd)
}

func (fwd forwardsToOption) ApplyLabeled(cfg *labeledOptions) {
	cfg.commonOpts.ForwardsTo = string(fwd)
}

func defaultForwardsTo() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "<unknown>"
	}

	exe, err := os.Executable()
	if err != nil {
		exe = "<unknown>"
	} else {
		exe = filepath.ToSlash(exe)
	}

	pid := os.Getpid()

	return fmt.Sprintf("app://%s/%s?pid=%d", hostname, exe, pid)
}
