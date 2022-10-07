package modules

// WithWebsocketTCPConversion enables the websocket-to-tcp converter.
func WithWebsocketTCPConversion() HTTPOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.WebsocketTCPConversion = true
	})
}
