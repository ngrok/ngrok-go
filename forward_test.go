package ngrok

import (
	"errors"
	"io"
	"net"
	"testing"

	"github.com/inconshreveable/log15/v3"
	"github.com/stretchr/testify/require"
)

func TestHalfCloseJoin(t *testing.T) {
	srv, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	waitSrvConn := make(chan net.Conn)
	go func() {
		srvConn, err := srv.Accept()
		if err != nil {
			panic(err)
		}
		waitSrvConn <- srvConn
	}()

	browser, ngrokEndpoint := net.Pipe()
	agent, userService := net.Pipe()

	waitJoinDone := make(chan struct{})
	go func() {
		defer close(waitJoinDone)
		join(log15.New(), ngrokEndpoint, agent)
	}()

	_, err = browser.Write([]byte("hello world"))
	require.NoError(t, err)
	var b [len("hello world")]byte
	_, err = userService.Read(b[:])
	require.NoError(t, err)
	require.Equal(t, []byte("hello world"), b[:])
	browser.Close()
	_, err = userService.Read(b[:])
	require.Truef(t, errors.Is(err, io.EOF), "io.EOF expected, got %v", err)

	<-waitJoinDone
}
