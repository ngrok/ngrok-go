package libngrok

import "github.com/ngrok/libngrok-go/internal/tunnel/proto"

type LabeledConfig struct {
	Labels     map[string]string
	Metadata   string
	ForwardsTo string
}

func LabeledOptions() *LabeledConfig {
	opts := &LabeledConfig{
		Labels: map[string]string{},
	}
	return opts
}

func (lo *LabeledConfig) WithLabel(key, value string) *LabeledConfig {
	lo.Labels[key] = value
	return lo
}

func (lo *LabeledConfig) WithForwardsTo(addr string) *LabeledConfig {
	lo.ForwardsTo = addr
	return lo
}

func (lo *LabeledConfig) WithMetadata(meta string) *LabeledConfig {
	lo.Metadata = meta
	return lo
}

func (lo *LabeledConfig) tunnelConfig() tunnelConfig {
	return tunnelConfig{
		forwardsTo: lo.ForwardsTo,
		labels:     lo.Labels,
		extra: proto.BindExtra{
			Metadata: lo.Metadata,
		},
	}
}
