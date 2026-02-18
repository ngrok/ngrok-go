package ngrok

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// httpJoin performs HTTP-aware bidirectional copying between proxy and backend.
// It parses HTTP request/response cycles and emits EventHTTPRequestComplete for each one.
// For non-HTTP traffic (e.g. after WebSocket upgrade), it falls back to raw copying.
func (e *endpointForwarder) httpJoin(proxy, backend net.Conn) {
	proxyBuf := bufio.NewReader(proxy)
	backendBuf := bufio.NewReader(backend)

	defer func() { _ = proxy.Close() }()
	defer func() { _ = backend.Close() }()

	for {
		startTime := time.Now()

		// Read request from proxy
		req, err := http.ReadRequest(proxyBuf)
		if err != nil {
			break
		}

		// Forward request to backend
		err = req.Write(backend)
		_ = req.Body.Close()
		if err != nil {
			break
		}

		// Read response from backend
		resp, err := http.ReadResponse(backendBuf, req)
		if err != nil {
			break
		}

		isUpgrade := resp.StatusCode == http.StatusSwitchingProtocols

		// Forward response to proxy
		err = resp.Write(proxy)
		_ = resp.Body.Close()
		if err != nil {
			break
		}

		// Emit HTTP request complete event
		e.emitConnectionEvent(newHTTPRequestComplete(
			e, req.Method, req.URL.RequestURI(), resp.StatusCode, time.Since(startTime),
		))

		// After protocol upgrade (e.g. WebSocket), fall back to raw copy
		if isUpgrade {
			e.joinBuffered(proxyBuf, proxy, backendBuf, backend)
			return
		}

		// Check if connection should close
		if resp.Close {
			break
		}
	}
}

// joinBuffered performs raw bidirectional copy using buffered readers.
// Used after WebSocket upgrade when there may be buffered data in the readers.
func (e *endpointForwarder) joinBuffered(proxyBuf *bufio.Reader, proxy net.Conn, backendBuf *bufio.Reader, backend net.Conn) {
	wg := &sync.WaitGroup{}

	wg.Go(func() {
		defer func() { _ = backend.Close() }()
		_, _ = io.Copy(backend, proxyBuf)
	})

	wg.Go(func() {
		defer func() { _ = proxy.Close() }()
		_, _ = io.Copy(proxy, backendBuf)
	})

	wg.Wait()
}
