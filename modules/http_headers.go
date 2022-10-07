package modules

import (
	"fmt"

	"github.com/ngrok/ngrok-go/internal/pb_agent"
)

// HTTP Headers to modify at the ngrok edge.
type Headers struct {
	// Headers to add to requests or responses at the ngrok edge.
	Added map[string]string
	// Header names to remove from requests or responses at the ngrok edge.
	Removed []string
}

// Add a header to all requests or responses at the ngrok edge.
// Inserts an entry into the [Headers].Added map.
func (h *Headers) Add(name, value string) *Headers {
	if h.Added == nil {
		h.Added = map[string]string{}
	}

	h.Added[name] = value
	return h
}

// Add a header to be removed from all requests or responses at the ngrok edge.
// Appends to the [Headers].Removed slice.
func (h *Headers) Remove(name ...string) *Headers {
	h.Removed = append(h.Removed, name...)
	return h
}

func (h *Headers) toProtoConfig() *pb_agent.MiddlewareConfiguration_Headers {
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

// HTTPHeaders constructs a new set of [Headers] for modification at the ngrok edge.
func HTTPHeaders() *Headers {
	return &Headers{
		Added:   map[string]string{},
		Removed: []string{},
	}
}

func (h *Headers) merge(other *Headers) *Headers {
	if h == nil {
		h = HTTPHeaders()
	}

	if other == nil {
		return h
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

// WithRequestHeaders configures the request headers for addition or removal at
// the ngrok edge.
func WithRequestHeaders(headers *Headers) HTTPOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.RequestHeaders = cfg.RequestHeaders.merge(headers)
	})
}

// WithResponseHeaders configures the response headers for addition or removal
// at the ngrok edge.
func WithResponseHeaders(headers *Headers) HTTPOption {
	return httpOptionFunc(func(cfg *httpOptions) {
		cfg.ResponseHeaders = cfg.ResponseHeaders.merge(headers)
	})
}
