// Naiive ngrok agent implementation.
// Sets up a single tunnel and forwards it to another service.

package main

import (
	"context"
	"io"
	"log"
	"net"
	"os"

	"github.com/ngrok/ngrok-go"
	"golang.org/x/sync/errgroup"
)

func usage(bin string) {
	log.Fatalf("Usage: %s <address:port>", bin)
}

func main() {
	if len(os.Args) != 2 {
		usage(os.Args[0])
	}
	if err := run(context.Background(), os.Args[1]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, dest string) error {
	_, tun, err := ngrok.ConnectAndStartTunnel(ctx,
		ngrok.ConnectOptions().
			WithAuthtoken(os.Getenv("NGROK_AUTHTOKEN")),
		ngrok.HTTPOptions(),
	)
	if err != nil {
		return err
	}

	log.Println("tunnel created:", tun.URL())

	l := tun.AsListener()

	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}

		log.Println("accepted connection from", conn.RemoteAddr())

		go func() {
			err := handleConn(ctx, dest, conn)
			log.Println("connection closed:", err)
		}()
	}
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
