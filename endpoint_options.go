package ngrok

import (
	"crypto/tls"
	"fmt"
	"net/url"

	"golang.ngrok.com/ngrok/v2/internal/legacy/config"
)

// EndpointOption is a functional option used to configure endpoints.
type EndpointOption func(*endpointOpts)

// endpointOpts stores configuration for endpoints.
type endpointOpts struct {
	poolingEnabled          bool
	bindings                []string
	description             string
	metadata                string
	agentTLSConfig          *tls.Config
	trafficPolicy           string
	url                     string
	upstreamProtocol        string
	upstreamURL             string
	upstreamTLSClientConfig *tls.Config
	proxyProtoVersion       config.ProxyProtoVersion
}

// defaultEndpointOpts returns the default options for an endpoint.
func defaultEndpointOpts() *endpointOpts {
	return &endpointOpts{}
}

// WithPoolingEnabled controls whether the endpoint supports connection pooling.
//
// See https://ngrok.com/docs/universal-gateway/endpoint-pooling/
func WithPoolingEnabled(pool bool) EndpointOption {
	return func(opts *endpointOpts) {
		opts.poolingEnabled = pool
	}
}

// WithBindings sets the endpoint's bindings.
//
// See https://ngrok.com/docs/universal-gateway/bindings/
func WithBindings(bindings ...string) EndpointOption {
	return func(opts *endpointOpts) {
		opts.bindings = bindings
	}
}

// WithDescription sets a human-readable description for the endpoint.
func WithDescription(desc string) EndpointOption {
	return func(opts *endpointOpts) {
		opts.description = desc
	}
}

// WithMetadata sets opaque, machine-readable metadata for the endpoint.
func WithMetadata(meta string) EndpointOption {
	return func(opts *endpointOpts) {
		opts.metadata = meta
	}
}

// WithAgentTLSTermination sets a TLS configuration that the agent will use to
// terminate connections received on the Endpoint.
//
// See https://ngrok.com/docs/agent/agent-tls-termination/
func WithAgentTLSTermination(config *tls.Config) EndpointOption {
	return func(opts *endpointOpts) {
		opts.agentTLSConfig = config
	}
}

// WithTrafficPolicy defines the Endpoint's Traffic Policy.
//
// See https://ngrok.com/docs/traffic-policy/
func WithTrafficPolicy(policy string) EndpointOption {
	return func(opts *endpointOpts) {
		opts.trafficPolicy = policy
	}
}

// WithURL defines the Endpoint's URL.
func WithURL(urlSpec string) EndpointOption {
	return func(opts *endpointOpts) {
		opts.url = urlSpec
	}
}

// temporary while we're wrapping the legacy api. remove this after we no longer
// call it so that new schemes
type endpointURLScheme string

const (
	httpScheme  endpointURLScheme = "http"
	httpsScheme endpointURLScheme = "https"
	tcpScheme   endpointURLScheme = "tcp"
	tlsScheme   endpointURLScheme = "tls"
)

// configureEndpoint creates the appropriate tunnel configuration based on the URL
// scheme and options
func configureEndpoint(scheme endpointURLScheme, endpointOpts *endpointOpts) (config.Tunnel, error) {
	switch scheme {
	case httpScheme, httpsScheme:
		return configureHTTPEndpoint(endpointOpts)
	case tcpScheme:
		return configureTCPEndpoint(endpointOpts)
	case tlsScheme:
		return configureTLSEndpoint(endpointOpts)
	default:
		return nil, fmt.Errorf("unsupported endpoint URL scheme: %s", scheme)
	}
}

// configureHTTPEndpoint configures an HTTP/HTTPS endpoint with options
func configureHTTPEndpoint(endpointOpts *endpointOpts) (config.Tunnel, error) {
	configOpts := []config.HTTPEndpointOption{}

	// Set URL and scheme if specified
	if endpointOpts.url != "" {
		configOpts = append(configOpts, config.WithURL(endpointOpts.url))

		// Parse the URL and always set scheme explicitly
		if parsedURL, err := url.Parse(endpointOpts.url); err == nil {
			// Determine scheme - default to HTTPS if not specified or is https
			scheme := config.SchemeHTTPS
			if parsedURL.Scheme == "http" {
				scheme = config.SchemeHTTP
			}
			configOpts = append(configOpts, config.WithScheme(scheme))
		}
	}

	// Set pooling if enabled
	if endpointOpts.poolingEnabled {
		configOpts = append(configOpts, config.WithPoolingEnabled(endpointOpts.poolingEnabled))
	}

	// Add bindings if specified
	if len(endpointOpts.bindings) > 0 {
		configOpts = append(configOpts, config.WithBindings(endpointOpts.bindings...))
	}

	// Add metadata if specified
	if len(endpointOpts.metadata) > 0 {
		configOpts = append(configOpts, config.WithMetadata(endpointOpts.metadata))
	}

	// Add description if specified
	if len(endpointOpts.description) > 0 {
		configOpts = append(configOpts, config.WithDescription(endpointOpts.description))
	}

	// Set traffic policy if specified
	if len(endpointOpts.trafficPolicy) > 0 {
		configOpts = append(configOpts, config.WithTrafficPolicy(endpointOpts.trafficPolicy))
	}

	// Set proxy protocol if specified
	if endpointOpts.proxyProtoVersion != config.ProxyProtoNone {
		configOpts = append(configOpts, config.WithProxyProto(endpointOpts.proxyProtoVersion))
	}

	// Note: upstreamVerifyTLSCAs is not currently supported in the legacy SDK
	// We'll need to implement this in a future version

	return config.HTTPEndpoint(configOpts...), nil
}

// configureTCPEndpoint configures a TCP endpoint with options
func configureTCPEndpoint(endpointOpts *endpointOpts) (config.Tunnel, error) {
	configOpts := []config.TCPEndpointOption{}

	// Set URL if specified
	if endpointOpts.url != "" {
		configOpts = append(configOpts, config.WithURL(endpointOpts.url))
	}

	// Set pooling if enabled
	if endpointOpts.poolingEnabled {
		configOpts = append(configOpts, config.WithPoolingEnabled(endpointOpts.poolingEnabled))
	}

	// Add bindings if specified
	if len(endpointOpts.bindings) > 0 {
		configOpts = append(configOpts, config.WithBindings(endpointOpts.bindings...))
	}

	// Add metadata if specified
	if len(endpointOpts.metadata) > 0 {
		configOpts = append(configOpts, config.WithMetadata(endpointOpts.metadata))
	}

	// Add description if specified
	if len(endpointOpts.description) > 0 {
		configOpts = append(configOpts, config.WithDescription(endpointOpts.description))
	}

	// Set traffic policy if specified
	if len(endpointOpts.trafficPolicy) > 0 {
		configOpts = append(configOpts, config.WithTrafficPolicy(endpointOpts.trafficPolicy))
	}

	// Set proxy protocol if specified
	if endpointOpts.proxyProtoVersion != config.ProxyProtoNone {
		configOpts = append(configOpts, config.WithProxyProto(endpointOpts.proxyProtoVersion))
	}

	return config.TCPEndpoint(configOpts...), nil
}

// configureTLSEndpoint configures a TLS endpoint with options
func configureTLSEndpoint(endpointOpts *endpointOpts) (config.Tunnel, error) {
	configOpts := []config.TLSEndpointOption{}

	// Set URL if specified
	if endpointOpts.url != "" {
		configOpts = append(configOpts, config.WithURL(endpointOpts.url))
	}

	// Set pooling if enabled
	if endpointOpts.poolingEnabled {
		configOpts = append(configOpts, config.WithPoolingEnabled(endpointOpts.poolingEnabled))
	}

	// Add bindings if specified
	if len(endpointOpts.bindings) > 0 {
		configOpts = append(configOpts, config.WithBindings(endpointOpts.bindings...))
	}

	// Add metadata if specified
	if len(endpointOpts.metadata) > 0 {
		configOpts = append(configOpts, config.WithMetadata(endpointOpts.metadata))
	}

	// Add description if specified
	if len(endpointOpts.description) > 0 {
		configOpts = append(configOpts, config.WithDescription(endpointOpts.description))
	}

	// Set traffic policy if specified
	if len(endpointOpts.trafficPolicy) > 0 {
		configOpts = append(configOpts, config.WithTrafficPolicy(endpointOpts.trafficPolicy))
	}

	// Set proxy protocol if specified
	if endpointOpts.proxyProtoVersion != config.ProxyProtoNone {
		configOpts = append(configOpts, config.WithProxyProto(endpointOpts.proxyProtoVersion))
	}

	return config.TLSEndpoint(configOpts...), nil
}

// determineURLScheme examines the URL to determine what scheme to use
func determineURLScheme(urlStr string) (endpointURLScheme, error) {
	if urlStr == "" {
		// Default to HTTPS if no URL specified
		return "https", nil
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("invalid URL format: %w", err)
	}

	// If no scheme is specified, default to HTTPS
	if parsedURL.Scheme == "" {
		return "https", nil
	}

	// Validate supported schemes
	switch parsedURL.Scheme {
	case "http", "https", "tcp", "tls":
		return endpointURLScheme(parsedURL.Scheme), nil
	default:
		return "", fmt.Errorf("unsupported endpoint URL scheme: %s", parsedURL.Scheme)
	}
}
