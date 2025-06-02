package ngrok

import (
	"crypto/tls"
)

// Upstream represents configuration for forwarding to an upstream service.
type Upstream struct {
	addr            string
	protocol        string
	proxyProto      ProxyProtoVersion
	tlsClientConfig *tls.Config
	dialer          Dialer
}

// UpstreamOption configures an Upstream instance.
type UpstreamOption func(*Upstream)

// WithUpstream creates an Upstream configuration with a required address.
// The address can be in various formats such as:
// - "80" (a port number for local services)
// - "example.com:8080" (a host:port combination)
// - "http://example.com" (a full URL)
func WithUpstream(addr string, opts ...UpstreamOption) *Upstream {
	opt := &Upstream{addr: addr}
	for _, o := range opts {
		o(opt)
	}
	return opt
}

// WithUpstreamProtocol sets the protocol to use when forwarding to the upstream.
// This is typically used to specify "http2" when communicating with an
// upstream HTTP/2 server.
func WithUpstreamProtocol(proto string) UpstreamOption {
	return func(o *Upstream) {
		o.protocol = proto
	}
}

// WithUpstreamTLSClientConfig sets the TLS client configuration to use when connecting
// to the upstream server over TLS.
func WithUpstreamTLSClientConfig(config *tls.Config) UpstreamOption {
	return func(o *Upstream) {
		o.tlsClientConfig = config
	}
}

// ProxyProtoVersion represents PROXY protocol version
type ProxyProtoVersion string

const (
	ProxyProtoV1 ProxyProtoVersion = "v1"
	ProxyProtoV2 ProxyProtoVersion = "v2"
)

// WithUpstreamProxyProto sets the PROXY protocol version to use when connecting
// to the upstream server. Valid values are ProxyProtoV1 or ProxyProtoV2.
//
// See https://ngrok.com/docs/agent/config/v3/#upstreamproxy_protocol
func WithUpstreamProxyProto(proxyProto ProxyProtoVersion) UpstreamOption {
	return func(o *Upstream) {
		o.proxyProto = proxyProto
	}
}

// WithUpstreamDialer sets a custom dialer to use when connecting to the upstream server.
// This allows for custom network configurations or connection methods when reaching the upstream.
func WithUpstreamDialer(dialer Dialer) UpstreamOption {
	return func(o *Upstream) {
		o.dialer = dialer
	}
}
