package ngrok

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestForwarderRejectsMalformedProxyHeader(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version ProxyProtoVersion
		header  string
	}{
		{"v1-magic", ProxyProtoV1, "BROKEN TCP4 192.0.2.1 192.0.2.2 1234 443\r\n"},
		{"v1-overlong", ProxyProtoV1, "PROXY " + strings.Repeat("x", 108) + "\r\n"},
		{"v1-truncated", ProxyProtoV1, "PROXY UNKNOWN\r"},
		{"v1-garbage", ProxyProtoV1, "PROXY garbage\r\n"},
		{"v1-missing-field", ProxyProtoV1, "PROXY TCP4 192.0.2.1 192.0.2.2 443\r\n"},
		{"v1-extra-field", ProxyProtoV1, "PROXY TCP4 192.0.2.1 192.0.2.2 1234 443 extra\r\n"},
		{"v1-spacing", ProxyProtoV1, "PROXY TCP4  192.0.2.1 192.0.2.2 1234 443\r\n"},
		{"v1-ip", ProxyProtoV1, "PROXY TCP4 999.0.2.1 192.0.2.2 1234 443\r\n"},
		{"v1-ip-leading-zero", ProxyProtoV1, "PROXY TCP4 192.00.2.1 192.0.2.2 1234 443\r\n"},
		{"v1-source-family", ProxyProtoV1, "PROXY TCP4 ::1 192.0.2.2 1234 443\r\n"},
		{"v1-destination-family", ProxyProtoV1, "PROXY TCP6 ::1 192.0.2.2 1234 443\r\n"},
		{"v1-zone", ProxyProtoV1, "PROXY TCP6 fe80::1%eth0 ::1 1234 443\r\n"},
		{"v1-port-range", ProxyProtoV1, "PROXY TCP4 192.0.2.1 192.0.2.2 65536 443\r\n"},
		{"v1-port-sign", ProxyProtoV1, "PROXY TCP4 192.0.2.1 192.0.2.2 1234 +443\r\n"},
		{"v1-port-leading-zero", ProxyProtoV1, "PROXY TCP4 192.0.2.1 192.0.2.2 01234 443\r\n"},
		{"v1-port-text", ProxyProtoV1, "PROXY TCP4 192.0.2.1 192.0.2.2 1234 abc\r\n"},
		{"v2-magic", ProxyProtoV2, "abcdefghijkl\x21\x00\x00\x00"},
		{"v2-version", ProxyProtoV2, "\r\n\r\n\x00\r\nQUIT\n\x11\x00\x00\x00"},
		{"v2-command", ProxyProtoV2, "\r\n\r\n\x00\r\nQUIT\n\x22\x00\x00\x00"},
		{"v2-truncated", ProxyProtoV2, "\r\n\r\n\x00\r\nQUIT\n\x21"},
		{"v2-payload", ProxyProtoV2, "\r\n\r\n\x00\r\nQUIT\n\x21\x11\x00\x0cshort"},
		{"v2-family", ProxyProtoV2, "\r\n\r\n\x00\r\nQUIT\n\x21\x41\x00\x00"},
		{"v2-protocol", ProxyProtoV2, "\r\n\r\n\x00\r\nQUIT\n\x21\x13\x00\x0c" + strings.Repeat("\x00", 12)},
		{"v2-short-ipv4", ProxyProtoV2, "\r\n\r\n\x00\r\nQUIT\n\x21\x11\x00\x0b" + strings.Repeat("\x00", 11)},
		{"v2-short-ipv6", ProxyProtoV2, "\r\n\r\n\x00\r\nQUIT\n\x21\x22\x00\x23" + strings.Repeat("\x00", 35)},
		{"v2-short-unix", ProxyProtoV2, "\r\n\r\n\x00\r\nQUIT\n\x21\x31\x00\xd7" + strings.Repeat("\x00", 215)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, rec := newRecordedForwarder(t, "tls://127.0.0.1:1")
			e.proxyProtocol = tc.version
			client, edge := net.Pipe()
			require.NoError(t, client.SetDeadline(time.Now().Add(5*time.Second)))
			done := make(chan struct{})
			go func() { defer close(done); e.handleConnection(context.Background(), edge) }()
			_, _ = io.WriteString(client, tc.header)
			_ = client.Close()
			<-done
			require.Empty(t, eventsOf[*EventUpstreamDialComplete](rec), "malformed header must not reach upstream")
		})
	}
}

func TestForwarderProxyHeaderBeforeTLS(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version ProxyProtoVersion
		header  string
	}{
		{"disabled", "", ""},
		{"v1", ProxyProtoV1, "PROXY TCP4 192.0.2.1 192.0.2.2 1234 443\r\n"},
		{"v1-ipv6", ProxyProtoV1, "PROXY TCP6 2001:db8::1 ::ffff:192.0.2.2 0 65535\r\n"},
		{"v1-unknown", ProxyProtoV1, "PROXY UNKNOWN\r\n"},
		{"v1-unknown-extra", ProxyProtoV1, "PROXY UNKNOWN arbitrary ignored fields\r\n"},
		{"v2", ProxyProtoV2, "\r\n\r\n\x00\r\nQUIT\n\x21\x11\x00\x0c\xc0\x00\x02\x01\xc0\x00\x02\x02\x04\xd2\x01\xbb"},
		{"v2-ipv4-dgram", ProxyProtoV2, "\r\n\r\n\x00\r\nQUIT\n\x21\x12\x00\x0c" + strings.Repeat("\x00", 12)},
		{"v2-ipv6-stream", ProxyProtoV2, "\r\n\r\n\x00\r\nQUIT\n\x21\x21\x00\x24" + strings.Repeat("\x00", 36)},
		{"v2-ipv6-dgram", ProxyProtoV2, "\r\n\r\n\x00\r\nQUIT\n\x21\x22\x00\x24" + strings.Repeat("\x00", 36)},
		{"v2-unix-stream", ProxyProtoV2, "\r\n\r\n\x00\r\nQUIT\n\x21\x31\x00\xd8" + strings.Repeat("\x00", 216)},
		{"v2-unix-dgram", ProxyProtoV2, "\r\n\r\n\x00\r\nQUIT\n\x21\x32\x00\xd8" + strings.Repeat("\x00", 216)},
		{"v2-unspec", ProxyProtoV2, "\r\n\r\n\x00\r\nQUIT\n\x21\x00\x00\x00"},
		{"v2-local", ProxyProtoV2, "\r\n\r\n\x00\r\nQUIT\n\x20\x00\x00\x00"},
		{"v2-local-opaque", ProxyProtoV2, "\r\n\r\n\x00\r\nQUIT\n\x20\xff\x00\x03abc"},
		{"v2-tlv", ProxyProtoV2, "\r\n\r\n\x00\r\nQUIT\n\x21\x11\x00\x12" + strings.Repeat("\x00", 12) + "\x04\x00\x03abc"},
	} {
		for _, mode := range []string{"tls", "https", "http-proxy", "tcp", "http"} {
			t.Run(fmt.Sprintf("%s/%s", tc.name, mode), func(t *testing.T) {
				header := []byte(tc.header)
				cert := httptest.NewTLSServer(http.NotFoundHandler())
				config := cert.TLS.Clone()
				cert.Close()
				ln, err := net.Listen("tcp", "127.0.0.1:0")
				require.NoError(t, err)
				defer ln.Close()
				require.NoError(t, ln.(*net.TCPListener).SetDeadline(time.Now().Add(5*time.Second)))
				result := make(chan error, 1)
				go func() {
					result <- func() error {
						conn, err := ln.Accept()
						if err != nil {
							return err
						}
						defer conn.Close()
						_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
						got := make([]byte, len(header))
						if _, err := io.ReadFull(conn, got); err != nil {
							return err
						}
						if string(got) != string(header) {
							return fmt.Errorf("plaintext PROXY header before TLS: got %x, want %x", got, header)
						}
						if mode != "tcp" && mode != "http" {
							conn = tls.Server(conn, config)
						}
						if mode == "http-proxy" || mode == "http" {
							req, err := http.ReadRequest(bufio.NewReader(conn))
							if err != nil {
								return err
							}
							_ = req.Body.Close()
							_, err = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
							return err
						}
						payload := make([]byte, 4)
						if _, err := io.ReadFull(conn, payload); err != nil {
							return err
						}
						if string(payload) != "ping" {
							return fmt.Errorf("payload: %q", payload)
						}
						_, err = io.WriteString(conn, "pong")
						return err
					}()
				}()
				scheme := mode
				if mode == "http-proxy" {
					scheme = "https"
				}
				e := newTestForwarder(t, scheme+"://"+ln.Addr().String())
				e.proxyProtocol = tc.version
				e.upstreamTLSClientConfig = &tls.Config{InsecureSkipVerify: true}
				e.endpointURL = url.URL{Scheme: "tcp"}
				if mode == "http-proxy" || mode == "http" {
					e.endpointURL.Scheme = "https"
				}
				client, edge := net.Pipe()
				_ = client.SetDeadline(time.Now().Add(5 * time.Second))
				done := make(chan struct{})
				go func() {
					defer close(done)
					e.handleConnection(context.Background(), edge)
				}()
				defer func() { _ = client.Close(); <-done }()
				request := "ping"
				if e.endpointURL.Scheme == "https" {
					request = "GET / HTTP/1.1\r\nHost: example.test\r\nConnection: close\r\n\r\n"
				}
				_, err = client.Write(append(append([]byte{}, header...), request...))
				if err != nil {
					t.Logf("edge write: %v", err)
				}
				require.NoError(t, <-result)
				if e.endpointURL.Scheme == "https" {
					resp, err := http.ReadResponse(bufio.NewReader(client), nil)
					require.NoError(t, err)
					defer resp.Body.Close()
					body, err := io.ReadAll(resp.Body)
					require.NoError(t, err)
					require.Equal(t, "ok", string(body))
				} else {
					reply := make([]byte, 4)
					_, err := io.ReadFull(client, reply)
					require.NoError(t, err)
					require.Equal(t, "pong", string(reply))
				}
			})
		}
	}
}
