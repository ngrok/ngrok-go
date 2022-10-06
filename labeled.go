package ngrok

// Options for labeled tunnels.
type LabeledConfig struct {
	// Common tunnel configuration options.
	CommonConfig *CommonConfig

	// A map of label, value pairs for this tunnel.
	Labels map[string]string
}

// Construct a new set of labeled tunnel options.
func LabeledOptions() *LabeledConfig {
	opts := &LabeledConfig{
		Labels:       map[string]string{},
		CommonConfig: &CommonConfig{},
	}
	return opts
}

// Add a label to this tunnel's set of label, value pairs.
func (cfg *LabeledConfig) WithLabel(label, value string) *LabeledConfig {
	if cfg.Labels == nil {
		cfg.Labels = map[string]string{}
	}

	cfg.Labels[label] = value
	return cfg
}

// Use the provided backend as the tunnel's ForwardsTo string.
// Sets the [CommonConfig].ForwardsTo field.
func (cfg *LabeledConfig) WithForwardsTo(backend string) *LabeledConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithForwardsTo(backend)
	return cfg
}

// Use the provided opaque metadata string for this tunnel.
// Sets the [CommonConfig].Metadata field.
func (cfg *LabeledConfig) WithMetadata(meta string) *LabeledConfig {
	cfg.CommonConfig = cfg.CommonConfig.WithMetadata(meta)
	return cfg
}

func (cfg *LabeledConfig) applyTunnelConfig(tcfg *tunnelConfig) {
	cfg.CommonConfig.applyTunnelConfig(tcfg)

	tcfg.labels = cfg.Labels
}
