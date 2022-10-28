package config

import (
	"fmt"

	"golang.ngrok.com/ngrok/internal/pb_agent"
)

// HTTP Headers to modify at the ngrok edge.
type headers struct {
	// Headers to add to requests or responses at the ngrok edge.
	Added map[string]string
	// Header names to remove from requests or responses at the ngrok edge.
	Removed []string
}

// Add a header to all requests or responses at the ngrok edge.
// Inserts an entry into the [Headers].Added map.
func (h *headers) Add(name, value string) *headers {
	if h.Added == nil {
		h.Added = map[string]string{}
	}

	h.Added[name] = value
	return h
}

// Add a header to be removed from all requests or responses at the ngrok edge.
// Appends to the [Headers].Removed slice.
func (h *headers) Remove(name ...string) *headers {
	h.Removed = append(h.Removed, name...)
	return h
}

func (h *headers) toProtoConfig() *pb_agent.MiddlewareConfiguration_Headers {
	if h == nil {
		return nil
	}

	headers := &pb_agent.MiddlewareConfiguration_Headers{
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

// WithRequestHeader adds a header to all requests to this edge.
func WithRequestHeader(name, value string) HTTPEndpointOption {
	return requestHeaders(headers{
		Added: map[string]string{name: value},
	})
}

// WithRequestHeader adds a header to all responses coming from this edge.
func WithResponseHeader(name, value string) HTTPEndpointOption {
	return responseHeaders(headers{
		Added: map[string]string{name: value},
	})
}

// WithRemoveRequestHeader removes a header from requests to this edge.
func WithRemoveRequestHeader(name string) HTTPEndpointOption {
	return requestHeaders(headers{
		Removed: []string{name},
	})
}

// WithRemoveResponseHeader removes a header from responses from this edge.
func WithRemoveResponseHeader(name string) HTTPEndpointOption {
	return responseHeaders(headers{
		Removed: []string{name},
	})
}
