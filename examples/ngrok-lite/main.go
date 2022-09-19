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

	"github.com/ngrok/libngrok-go"
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
	if len(os.Args) != 2 {
		usage(os.Args[0])
	}

	dest := os.Args[1]

	ctx := context.Background()
	sess := common.Unwrap(libngrok.Connect(ctx, libngrok.ConnectOptions().WithAuthToken(os.Getenv("NGROK_TOKEN"))))

	tun := common.Unwrap(sess.StartTunnel(ctx, libngrok.HTTPOptions()))

	fmt.Println("Tunnel created:", tun.URL())

	l := tun.AsListener()

	g := sync.WaitGroup{}

	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("failed to accept connection:", err)
			break
		}

		fmt.Println("Accepted connection from", conn.RemoteAddr())

		g.Add(1)
		go func() {
			err := handleConn(ctx, dest, conn)
			fmt.Println("connection closed:", err)
			g.Done()
		}()
	}

	g.Wait()
}
