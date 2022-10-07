package modules

// A URL scheme.
type Scheme string

// The 'http' URL scheme.
const (
	SchemeHTTP = Scheme("http")
	// The 'https' URL scheme.
	SchemeHTTPS = Scheme("https")
)

// WithScheme sets the scheme for this edge.
func WithScheme(scheme Scheme) HTTPOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.Scheme = scheme
	})
}
