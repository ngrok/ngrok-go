package ngrok

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/inconshreveable/log15/v3"
	"golang.org/x/sync/errgroup"
)

// Forwarder is a tunnel that has every connection forwarded to some URL.
type Forwarder interface {
	// Information about the tunnel being forwarded
	TunnelInfo

	// Close is a convenience method for calling Tunnel.CloseWithContext
	// with a context that has a timeout of 5 seconds. This also allows the
	// Tunnel to satisfy the io.Closer interface.
	Close() error

	// CloseWithContext closes the Tunnel. Closing a tunnel is an operation
	// that involves sending a "close" message over the parent session.
	// Since this is a network operation, it is most correct to provide a
	// context with a timeout.
	CloseWithContext(context.Context) error

	// Session returns the tunnel's parent Session object that it
	// was started on.
	Session() Session

	// Wait blocks until the forwarding task exits (usually due to tunnel
	// close), or the `context.Context` that it was started with is canceled.
	Wait() error
}

type forwarder struct {
	Tunnel
	mainGroup *errgroup.Group
}

func (fwd *forwarder) Wait() error {
	return fwd.mainGroup.Wait()
}

// compile-time check that we're implementing the proper interface
var _ Forwarder = (*forwarder)(nil)

func join(logger log15.Logger, left, right net.Conn) {
	g := &sync.WaitGroup{}
	g.Add(2)
	go func() {
		defer g.Done()
		defer left.Close()
		n, err := io.Copy(left, right)
		logger.Debug("left join finished", "err", err, "bytes", n)
	}()
	go func() {
		defer g.Done()
		defer right.Close()
		n, err := io.Copy(right, left)
		logger.Debug("right join finished", "err", err, "bytes", n)
	}()
	g.Wait()
}

func forwardTunnel(ctx context.Context, tun Tunnel, url *url.URL) Forwarder {
	mainGroup, ctx := errgroup.WithContext(ctx)
	fwdTasks := &sync.WaitGroup{}

	sess := tun.Session()
	sessImpl := sess.(*sessionImpl)
	logger := sessImpl.inner().Logger.New("task", "forward", "toUrl", url, "tunnelUrl", tun.URL())

	mainGroup.Go(func() error {
		for {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}

			conn, err := tun.Accept()
			if err != nil {
				return err
			}
			logger.Debug("accept connection from", "address", conn.RemoteAddr())
			fwdTasks.Add(1)

			go func() {
				ngrokConn := conn.(Conn)

				backend, err := openBackend(ctx, logger, tun, ngrokConn, url)
				if err != nil {
					defer ngrokConn.Close()
					logger.Warn("failed to connect to backend url", "error", err)
					fwdTasks.Done()
					return
				}

				join(logger.New("url", url), ngrokConn, backend)
				fwdTasks.Done()
			}()
		}
	})

	return &forwarder{
		Tunnel:    tun,
		mainGroup: mainGroup,
	}
}

// TODO: use an actual reverse proxy for http/s tunnels so that the host header gets set?
func openBackend(ctx context.Context, logger log15.Logger, tun Tunnel, tunnelConn Conn, url *url.URL) (net.Conn, error) {
	host := url.Hostname()
	port := url.Port()
	if port == "" {
		switch {
		case usesTLS(url.Scheme):
			port = "443"
		case isHTTP(url.Scheme):
			port = "80"
		default:
			return nil, fmt.Errorf("no default tcp port available for %s", url.Scheme)
		}
		logger.Debug("set default port", "port", port)
	}
	var appProto string
	if fwdProto, ok := tun.(interface{ ForwardsProto() string }); ok {
		appProto = fwdProto.ForwardsProto()
	}

	// Create TLS config if necessary
	var tlsConfig *tls.Config
	if usesTLS(url.Scheme) {
		tlsConfig = &tls.Config{
			ServerName:    url.Hostname(),
			Renegotiation: tls.RenegotiateOnceAsClient,
		}
		// If the backend is TLS and we've requested HTTP2, we'll need to
		// make the backend aware of that via ALPN.
		if appProto == "http2" {
			logger.Debug("negotiating http/2 via alpn")
			tlsConfig.NextProtos = append(tlsConfig.NextProtos, "h2", "http/1.1")
		}
	}

	dialer := &net.Dialer{}
	address := fmt.Sprintf("%s:%s", host, port)
	logger.Debug("dial backend tcp", "address", address)

	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		defer tunnelConn.Close()

		// TODO: this http error is only valid for http/1.1. If the edge is
		//       expecting http/2, it'll end up being a proxy error instead.
		//       We should probably find a better way to do this that doesn't involve
		//       understanding http here.
		if isHTTP(tunnelConn.Proto()) && appProto != "http2" {
			_ = writeHTTPError(tunnelConn, err)
		}
		return nil, err
	}

	if usesTLS(url.Scheme) && !tunnelConn.PassthroughTLS() {
		logger.Debug("establishing TLS connection with backend")
		return tls.Client(conn, tlsConfig), nil
	}

	return conn, nil
}

func writeHTTPError(w io.Writer, err error) error {
	resp := &http.Response{}
	resp.StatusCode = http.StatusBadGateway
	resp.Body = io.NopCloser(bytes.NewBufferString(fmt.Sprintf("failed to connect to backend: %s", err.Error())))
	return resp.Write(w)
}

func usesTLS(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "https", "tls":
		return true
	default:
		return false
	}
}

func isHTTP(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "https", "http":
		return true
	default:
		return false
	}
}
