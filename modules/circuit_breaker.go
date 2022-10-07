package modules

// WithCircuitBreaker sets the 5XX response ratio at which the ngrok edge will
// stop sending requests to this tunnel.
func WithCircuitBreaker(ratio float64) HTTPOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.CircuitBreaker = ratio
	})
}
