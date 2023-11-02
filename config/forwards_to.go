package config

import (
	"fmt"
	"os"
	"path/filepath"
)

type forwardsToOption string

// WithForwardsTo sets the ForwardsTo string for this tunnel.
// This can be viewed via the API or dashboard.
//
// This overrides the default process info if using
// [golang.ngrok.com/ngrok.Listen], and is in turn overridden by the url
// provided to [golang.ngrok.com/ngrok.ListenAndForward].
//
// https://ngrok.com/docs/api/resources/tunnels/#tunnel-fields
func WithForwardsTo(meta string) Options {
	return forwardsToOption(meta)
}

func (fwd forwardsToOption) ApplyCommon(cfg *commonOpts) {
	cfg.ForwardsTo = string(fwd)
}

func (fwd forwardsToOption) ApplyHTTP(cfg *httpOptions) {
	fwd.ApplyCommon(&cfg.commonOpts)
}

func (fwd forwardsToOption) ApplyTCP(cfg *tcpOptions) {
	fwd.ApplyCommon(&cfg.commonOpts)
}

func (fwd forwardsToOption) ApplyTLS(cfg *tlsOptions) {
	fwd.ApplyCommon(&cfg.commonOpts)
}

func (fwd forwardsToOption) ApplyLabeled(cfg *labeledOptions) {
	fwd.ApplyCommon(&cfg.commonOpts)
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
