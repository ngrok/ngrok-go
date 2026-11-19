package ngrok

import (
	"crypto/tls"
	"fmt"
	"net/url"
	"strings"
)

const (
	upstreamProtocolHTTP1 = "http1"
	upstreamProtocolHTTP2 = "http2"
)

// normalizeUpstreamProtocol normalizes and validates an upstream protocol value
// so the hint sent to the edge, the tunnel-side server, the upstream transport,
// and SetUpstream all agree.
func normalizeUpstreamProtocol(proto string) (string, error) {
	normalized := strings.ToLower(proto)
	switch normalized {
	case "", upstreamProtocolHTTP1, upstreamProtocolHTTP2:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported upstream protocol: %s", proto)
	}
}

// Upstream represents configuration for forwarding to an upstream service.
type Upstream struct {
	addr            string
	protocol        string
	proxyProto      ProxyProtoVersion
	tlsClientConfig *tls.Config
	dialer          Dialer
}

// Addr returns the address that WithUpstream was given.
func (u *Upstream) Addr() string { return u.addr }

// Dialer returns the dialer that WithUpstreamDialer set, or nil if the SDK
// dials the upstream.
func (u *Upstream) Dialer() Dialer { return u.dialer }

// target parses u's address into where a forwarder sends connections.
func (u *Upstream) target() (*upstreamTarget, error) {
	addr, err := url.Parse(u.addr)
	if err != nil {
		return nil, fmt.Errorf("invalid upstream URL: %w", err)
	}
	return &upstreamTarget{url: addr, dialer: u.dialer, tlsClientConfig: u.tlsClientConfig}, nil
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
// Valid values are "", "http1", and "http2" (case-insensitive); unrecognized
// values cause Forward and SetUpstream to return an error.
//
// Setting "http2" instructs the ngrok edge to send every request over HTTP/2
// for both http:// and https:// upstreams. WebSocket upgrades are rejected at
// the edge (ERR_NGROK_3004) on http2 endpoints; leave the protocol unset for
// upstreams that serve WebSockets.
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
