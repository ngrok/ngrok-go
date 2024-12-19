package config

import (
	"fmt"
	"net/http"

	"golang.ngrok.com/ngrok/internal/mw"
)

// HTTP Headers to modify at the ngrok edge.
type headers struct {
	// Headers to add to requests or responses at the ngrok edge.
	Added map[string]string
	// Header names to remove from requests or responses at the ngrok edge.
	Removed []string
}

func (h *headers) toProtoConfig() *mw.MiddlewareConfiguration_Headers {
	if h == nil {
		return nil
	}

	headers := &mw.MiddlewareConfiguration_Headers{
		Remove: h.Removed,
	}

	for k, v := range h.Added {
		headers.Add = append(headers.Add, fmt.Sprintf("%s:%s", k, v))
	}

	return headers
}

func (h *headers) merge(other headers) *headers {
	if h == nil {
		h = &headers{
			Added:   map[string]string{},
			Removed: []string{},
		}
	}

	for k, v := range other.Added {
		if existing, ok := h.Added[k]; ok {
			v = fmt.Sprintf("%s;%s", existing, v)
		}
		h.Added[k] = v
	}

	h.Removed = append(h.Removed, other.Removed...)

	return h
}

type requestHeaders headers
type responseHeaders headers

func (h requestHeaders) ApplyHTTP(cfg *httpOptions) {
	cfg.RequestHeaders = cfg.RequestHeaders.merge(headers(h))

}

func (h responseHeaders) ApplyHTTP(cfg *httpOptions) {
	cfg.ResponseHeaders = cfg.ResponseHeaders.merge(headers(h))
}

// WithHostHeaderRewrite will automatically set the `Host` header to the one in
// the URL passed to `ListenAndForward`. Does nothing if using `Listen`.
// Defaults to `false`.
//
// If you need to set the host header to a specific value, use
// `WithRequestHeader("host", "some.host.com")` instead.
func WithHostHeaderRewrite(rewrite bool) HTTPEndpointOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.RewriteHostHeader = rewrite
	})
}

// WithRequestHeader adds a header to all requests to this edge.
//
// https://ngrok.com/docs/http/request-headers/
func WithRequestHeader(name, value string) HTTPEndpointOption {
	return requestHeaders(headers{
		Added: map[string]string{http.CanonicalHeaderKey(name): value},
	})
}

// WithRequestHeader adds a header to all responses coming from this edge.
//
// https://ngrok.com/docs/http/response-headers/
func WithResponseHeader(name, value string) HTTPEndpointOption {
	return responseHeaders(headers{
		Added: map[string]string{http.CanonicalHeaderKey(name): value},
	})
}

// WithRemoveRequestHeader removes a header from requests to this edge.
//
// https://ngrok.com/docs/http/request-headers/
func WithRemoveRequestHeader(name string) HTTPEndpointOption {
	return requestHeaders(headers{
		Removed: []string{http.CanonicalHeaderKey(name)},
	})
}

// WithRemoveResponseHeader removes a header from responses from this edge.
//
// https://ngrok.com/docs/http/response-headers/
func WithRemoveResponseHeader(name string) HTTPEndpointOption {
	return responseHeaders(headers{
		Removed: []string{http.CanonicalHeaderKey(name)},
	})
}
