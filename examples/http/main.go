// A simple HTTP service.

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/ngrok/ngrok-go"
	"github.com/ngrok/ngrok-go/modules"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	_, tun, err := ngrok.ConnectAndStartTunnel(ctx,
		ngrok.ConnectOptions().
			WithAuthtoken(os.Getenv("NGROK_AUTHTOKEN")),
		modules.HTTPOptions(),
	)
	if err != nil {
		return err
	}

	log.Println("tunnel created:", tun.URL())

	return tun.AsHTTP().Serve(ctx, http.HandlerFunc(handler))
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello from ngrok-go!")
}
