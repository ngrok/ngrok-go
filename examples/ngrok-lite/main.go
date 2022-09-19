// Naiive ngrok agent implementation.
// Sets up a single tunnel and forwards it to another service.

package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"

	"github.com/inconshreveable/log15"
	"github.com/ngrok/ngrok-go"
	"github.com/ngrok/ngrok-go/examples/common"
	"golang.org/x/sync/errgroup"
)

func usage(bin string) {
	fmt.Printf("Usage: %s <address:port>\n", bin)
	os.Exit(1)
}

func handleConn(ctx context.Context, dest string, conn net.Conn) error {
	next, err := net.Dial("tcp", dest)
	if err != nil {
		return err
	}

	g, _ := errgroup.WithContext(ctx)

	g.Go(func() error {
		_, err := io.Copy(next, conn)
		return err
	})
	g.Go(func() error {
		_, err := io.Copy(conn, next)
		return err
	})

	return g.Wait()
}

func main() {
	log15.Root().SetHandler(log15.LvlFilterHandler(log15.LvlInfo, log15.StdoutHandler))

	if len(os.Args) != 2 {
		usage(os.Args[0])
	}

	dest := os.Args[1]

	ctx := context.Background()
	sess := common.Unwrap(ngrok.Connect(ctx, ngrok.ConnectOptions().
		WithAuthtoken(os.Getenv("NGROK_TOKEN"))))

	tun := common.Unwrap(sess.StartTunnel(ctx, ngrok.HTTPOptions()))

	log15.Info("tunnel created", "url", tun.URL())

	l := tun.AsListener()

	g := sync.WaitGroup{}

	for {
		conn, err := l.Accept()
		if err != nil {
			log15.Error("failed to accept connection", "error", err)
			break
		}

		log15.Info("accepted connection", "remote", conn.RemoteAddr())

		g.Add(1)
		go func() {
			err := handleConn(ctx, dest, conn)
			log15.Info("connection closed", "error", err)
			g.Done()
		}()
	}

	g.Wait()
}
