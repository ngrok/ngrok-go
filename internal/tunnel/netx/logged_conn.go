package netx

import (
	"net"

	log "github.com/inconshreveable/log15"
	logext "github.com/inconshreveable/log15/ext"
)

// LoggedConn is a connection with an embedded logger
type LoggedConn interface {
	net.Conn
	log.Logger

	Unwrap() net.Conn
}

type logged struct {
	net.Conn
	log.Logger
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

func NewLoggedConn(parent log.Logger, conn net.Conn, ctx ...any) LoggedConn {
	c := &logged{
		Conn: conn,
		id:   logext.RandId(6),
	}
	c.Logger = parent.New(append([]any{"id", c.id}, ctx...)...)
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
