package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"sync"
)

// connMetaContextKey is the key used to store conn meta http.Request.Context
// by this package
type connMetaContextKey struct{}

// ServerServeConn implements http.Server.ServeConn, see
// https://github.com/golang/go/issues/36673
type ServeConnServer struct {
	*http.Server
	log *slog.Logger

	l           *chanListener
	origHandler http.Handler
}

func NewServeConnServer(s *http.Server, log *slog.Logger) *ServeConnServer {
	l := &chanListener{
		conns:     make(chan net.Conn, 64),
		closingCh: make(chan struct{}),
		connMeta:  &sync.Map{},
	}

	origContext, origConnState, origHandler := s.ConnContext, s.ConnState, s.Handler

	srv := &ServeConnServer{s, log, l, origHandler}
	s.ConnContext = func(ctx context.Context, conn net.Conn) context.Context {
		if origContext != nil {
			ctx = origContext(ctx, conn)
		}
		return srv.connContext(ctx, conn)
	}

	s.ConnState = func(conn net.Conn, state http.ConnState) {
		if origConnState != nil {
			origConnState(conn, state)
		}
		srv.connState(conn, state)
	}

	s.Handler = srv

	return srv
}

// ListenAndServe begins serving. It always returns an error
// (http.ErrServerClosed)
func (s *ServeConnServer) ListenAndServe() error {
	defer s.l.Close()
	return s.Server.Serve(s.l)
}

func (s *ServeConnServer) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// also log panics, our http server swallows them because the http server is
	// really noisy if you do set 'http.Server.ErrorLog', but that's also where
	// panics go. Bleh. Log em ourselves.
	defer func() {
		if r := recover(); r != nil {
			rErr, ok := r.(error)
			switch {
			case ok && errors.Is(rErr, http.ErrAbortHandler):
				s.log.Debug("aborting handler with ErrAbortHandler panic")
			default:
				s.log.Error("panic from ServeHTTP handler", "panic", r, "stack", string(debug.Stack()))
			}
			// re-raise for the http server to squash it
			panic(r)
		}
	}()

	ac := req.Context().Value(connMetaContextKey{}).(*activeConnMetadata)

	if ac == nil {
		// Metadata was removed during shutdown - connection is closing
		s.log.Debug("connection metadata missing, likely due to shutdown")
		return
	}

	if !ac.shut.Do(func() {
		s.origHandler.ServeHTTP(rw, req)
	}) {
		// This can happen if our caller calls 'serv.Shutdown' after this `ServeHTTP` is called, but before we get to `ac.shut.Do`
		// We also have a couple code-paths, namely ctx canceling `ServeConn`,
		// which stops accepitng new connections too.
		s.log.Debug("race between ServeHTTP and connection closing")
	}
}

// ServeConn begins serving the given connection.
// Optionally, the provided 'state' will be made available via the
// ExtractConnServerState function.
// Note, the ctx is currently ignored. It should be respected in the future,
// but it fails unit tests, so it needs some more careful consideration.
// If an error is returned, it will be 'http.ErrServerClosed', indicating the
// connection was not served, or a context error.
func (s *ServeConnServer) ServeConn(ctx context.Context, conn net.Conn, state any) error {
	ac := &activeConnMetadata{
		state: state,
		shut:  Shutdown{},
	}
	if !s.l.AddConn(conn, ac) {
		return http.ErrServerClosed
	}
	defer s.l.RemoveConn(conn)

	go func() {
		// start terminating this connection if the `ctx` passed into ServeConn gets canceled
		<-ctx.Done()
		ac.shut.Shutdown()
	}()

	s.log.Debug("wait")
	<-ac.shut.C()
	return nil
}

func (s *ServeConnServer) connContext(ctx context.Context, conn net.Conn) context.Context {
	ac := s.l.ConnMeta(conn)
	ctx = context.WithValue(ctx, connMetaContextKey{}, ac)
	return ctx
}

func ExtractConnServerState(req *http.Request) any {
	ac := req.Context().Value(connMetaContextKey{}).(*activeConnMetadata)
	if ac != nil {
		return ac.state
	}
	return nil
}

func (s *ServeConnServer) connState(conn net.Conn, state http.ConnState) {
	switch state {
	case http.StateClosed, http.StateHijacked:
		s.log.Debug("state", "state", state)
		ac := s.l.ConnMeta(conn)
		if ac == nil {
			// During shutdown, the `StateClosed` and `ln.Close()` shutdown race, so we can get here with our connection metadata already cleaned up.
			return
		}
		// this is closed or hijacked, so this conn can't be used anymore by
		// ServeHTTP. Shutdown, let ServeHTTP finish, etc
		ac.shut.Shutdown()
	}
}

// chanListener is a shim that allows us to feed existing net.Conns
// an http.Server instead of the usual route where they can only be
// accepted off the network
type chanListener struct {
	conns chan net.Conn

	mu          sync.RWMutex
	closed      bool
	closingCh   chan struct{}
	closingOnce sync.Once

	// We need to smuggle metadata about the conn, but we're not allowed to use
	// our own conn type because the http.Server internally does a specific type
	// assertion to `*tls.Conn`, and that needs to pass for http/2 to work.
	// So here we are.
	connMeta *sync.Map
}

func (l *chanListener) AddConn(conn net.Conn, meta *activeConnMetadata) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.closed {
		return false
	}

	l.connMeta.Store(conn, meta)

	// Hold read lock while sending to coordinate with Close()
	select {
	case l.conns <- conn:
		return true
	case <-l.closingCh:
		// Close() is happening, clean up
		l.connMeta.Delete(conn)
		return false
	}
}

func (l *chanListener) ConnMeta(conn net.Conn) *activeConnMetadata {
	meta, ok := l.connMeta.Load(conn)
	if !ok {
		return nil
	}
	return meta.(*activeConnMetadata)
}

func (l *chanListener) RemoveConn(conn net.Conn) {
	l.connMeta.Delete(conn)
}

func (l *chanListener) Accept() (net.Conn, error) {
	conn, ok := <-l.conns
	if !ok {
		return nil, net.ErrClosed
	}
	return conn, nil
}

func (l *chanListener) Close() error {
	// Signal closing to unblock any AddConn calls waiting on the channel
	l.closingOnce.Do(func() {
		close(l.closingCh)
	})

	// Anonymous function to scope defer for early return
	func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.closed {
			return
		}
		l.closed = true
		close(l.conns)
	}()

	// Shutdown all connections (Shutdown doesn't block)
	l.connMeta.Range(func(key, value any) bool {
		ac := value.(*activeConnMetadata)
		ac.shut.Shutdown()
		return true
	})
	return nil
}

var mockAddr = &net.TCPAddr{
	IP:   net.IPv4(0, 0, 0, 0),
	Port: 443,
}

func (l *chanListener) Addr() net.Addr {
	return mockAddr
}

type activeConnMetadata struct {
	state any
	shut  Shutdown
}
