package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"golang.ngrok.com/ngrok/v2"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	ln, err := ngrok.Listen(ctx)
	if err != nil {
		return err
	}

	log.Println("endpoint online", ln.URL())

	// Serve HTTP traffic on the ngrok endpoint
	return http.Serve(ln, http.HandlerFunc(handler))
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello from ngrok-go!")
}
