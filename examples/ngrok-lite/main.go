// Naïve ngrok agent implementation.
// Sets up a single tunnel and forwards it to another service.

package main

import (
	"context"
	"io"
	"log"
	"net"
	"os"

	"golang.org/x/sync/errgroup"

	"golang.ngrok.com/ngrok"
	"golang.ngrok.com/ngrok/config"
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
	tun, err := ngrok.StartTunnel(ctx,
		config.HTTPEndpoint(),
		ngrok.WithAuthtokenFromEnv(),
	)
	if err != nil {
		return err
	}

	log.Println("tunnel created:", tun.URL())

	for {
		conn, err := tun.Accept()
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
