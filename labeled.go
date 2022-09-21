package libngrok

import "github.com/ngrok/libngrok-go/internal/tunnel/proto"

type LabeledConfig struct {
	CommonConfig *CommonConfig

	Labels map[string]string
}

func LabeledOptions() *LabeledConfig {
	opts := &LabeledConfig{
		Labels:       map[string]string{},
		CommonConfig: &CommonConfig{},
	}
	return opts
}

func (cfg *LabeledConfig) WithLabel(key, value string) *LabeledConfig {
	cfg.Labels[key] = value
	return cfg
}

func (cfg *LabeledConfig) WithForwardsTo(addr string) *LabeledConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithForwardsTo(addr)
	return cfg
}

func (cfg *LabeledConfig) WithMetadata(meta string) *LabeledConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithMetadata(meta)
	return cfg
}

func (cfg *LabeledConfig) tunnelConfig() tunnelConfig {
	return tunnelConfig{
		forwardsTo: cfg.CommonConfig.ForwardsTo,
		labels:     cfg.Labels,
		extra: proto.BindExtra{
			Metadata: cfg.CommonConfig.Metadata,
		},
	}
}
