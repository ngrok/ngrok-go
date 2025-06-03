package netx

import (
	"fmt"
	"log/slog"
	"math/rand"
	"net"
)

// LoggedConn is a connection with an embedded logger
type LoggedConn interface {
	net.Conn

	// the subset of log methods from *slog.Logger that we use; feel free to add
	// more

	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)

	Unwrap() net.Conn
}

type logged struct {
	net.Conn
	*slog.Logger
	id string
}

type closeReader interface {
	CloseRead() error
}

type closeWriter interface {
	CloseWrite() error
}

func (c *logged) String() string {
	return "conn:" + c.id
}

func (c *logged) Unwrap() net.Conn {
	return c.Conn
}

func NewLoggedConn(parent *slog.Logger, conn net.Conn, ctx ...any) LoggedConn {
	id := rand.Uint64()
	c := &logged{
		Conn: conn,
		id:   fmt.Sprintf("%x", id),
	}
	c.Logger = parent.With(append([]any{"id", c.id}, ctx...)...)
	if _, ok := conn.(closeReader); !ok {
		return c
	}
	if _, ok := conn.(closeWriter); !ok {
		return c
	}
	return &loggedCloser{c}
}

func (c *logged) Close() (err error) {
	if err := c.Conn.Close(); err == nil {
		c.Debug("close", "err", err)
	}
	return
}

type loggedCloser struct {
	*logged
}

func (c *loggedCloser) CloseRead() error {
	return c.Conn.(closeReader).CloseRead()
}

func (c *loggedCloser) CloseWrite() error {
	return c.Conn.(closeWriter).CloseWrite()
}
