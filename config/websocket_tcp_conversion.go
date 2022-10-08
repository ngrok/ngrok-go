package config

// WithWebsocketTCPConversion enables the websocket-to-tcp converter.
func WithWebsocketTCPConversion() HTTPEndpointOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.WebsocketTCPConversion = true
	})
}
