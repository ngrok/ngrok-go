package config

// WithWebsocketTCPConversion enables the websocket-to-tcp converter.
//
// https://ngrok.com/docs/http/websocket-tcp-converter/
func WithWebsocketTCPConversion() HTTPEndpointOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.WebsocketTCPConversion = true
	})
}
