package config

// WithCompression enables gzip compression.
func WithCompression() HTTPEndpointOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.Compression = true
	})
}
