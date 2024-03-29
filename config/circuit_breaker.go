package config

// WithCircuitBreaker sets the 5XX response ratio at which the ngrok edge will
// stop sending requests to this tunnel.
//
// https://ngrok.com/docs/http/circuit-breaker/
func WithCircuitBreaker(ratio float64) HTTPEndpointOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.CircuitBreaker = ratio
	})
}
