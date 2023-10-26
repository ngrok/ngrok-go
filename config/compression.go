package config

// WithCompression enables gzip compression.
//
// https://ngrok.com/docs/http/compression/
func WithCompression() HTTPEndpointOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.Compression = true
	})
}
