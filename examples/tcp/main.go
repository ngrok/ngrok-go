// Simple TCP echo service.

package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"

	"golang.ngrok.com/ngrok/v2"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	ln, err := ngrok.Listen(ctx, ngrok.WithURL("tcp://"))
	if err != nil {
		return err
	}
	log.Println("Endpoint online", ln.URL())
	return runListener(ctx, ln)
}

func runListener(ctx context.Context, l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}

		log.Println("accepted connection from", conn.RemoteAddr())

		go func() {
			log.Println("connection closed:", handleConn(ctx, conn))
		}()
	}
}

func handleConn(ctx context.Context, conn net.Conn) error {
	_, err := fmt.Fprintln(conn, "Hello from ngrok-go!")
	if err != nil {
		return err
	}

	_, err = io.Copy(conn, conn)
	return err
}
