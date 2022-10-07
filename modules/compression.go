package modules

// WithCompression enables gzip compression.
func WithCompression() HTTPOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.Compression = true
	})
}
