package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"golang.ngrok.com/ngrok/v2"
)

// how to invoke this example:
// ./forward https://mydomain.ngrok.app http://localhost:8080
func main() {
	if err := run(context.Background(), os.Args[1], os.Args[2]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, from string, to string) error {
	// Forward using the agent's Forward method
	fwd, err := ngrok.Forward(ctx,
		ngrok.WithUpstream(to),
		ngrok.WithURL(from),
	)
	if err != nil {
		return err
	}

	fmt.Println("endpoint online: forwarding from", fwd.URL(), "to", to)

	// forwarding lasts indefinitely unless you explicitly stop it so this will
	// never return unless there's an unrecoverable error like running out of
	// memory or file descriptors
	// Wait for the forwarding to complete
	<-fwd.Done()
	return nil
}
