package modules

// A URL scheme.
type Scheme string

// The 'http' URL scheme.
const (
	SchemeHTTP = Scheme("http")
	// The 'https' URL scheme.
	SchemeHTTPS = Scheme("https")
)

// Use the provided scheme for this edge.
// Sets the [httpOptions].Scheme field.
func WithScheme(scheme Scheme) HTTPOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.Scheme = scheme
	})
}
