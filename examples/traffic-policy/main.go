// Example demonstrating how to use ngrok's traffic policy feature
// to implement rate limiting on an HTTP endpoint.

package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"

	"golang.ngrok.com/ngrok/v2"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

const trafficPolicy = `
on_http_request:
- actions:
  - type: rate-limit
    config:
      name: client-ip-limit
      algorithm: sliding_window
      capacity: 5
      rate: "60s"
      bucket_key:
      - conn.client_ip
  - type: basic-auth
    config:
      credentials:
      - username1:some-secret-1
      - username2:some-secret-2
  - type: add-headers
    config:
      headers:
        authenticated-user: "${actions.ngrok.basic_auth.credentials.username}"
`

func run(ctx context.Context) error {
	// Create an HTTP listener with the traffic policy
	agent, err := ngrok.NewAgent(
		ngrok.WithAuthtoken(os.Getenv("NGROK_AUTHTOKEN")),
		ngrok.WithLogger(slog.Default()),
	)
	if err != nil {
		return err
	}
	ln, err := agent.Listen(ctx,
		ngrok.WithTrafficPolicy(trafficPolicy),
		ngrok.WithDescription("traffic policy example"),
	)
	if err != nil {
		return err
	}
	// Serve HTTP traffic on the ngrok endpoint
	log.Println("Endpoint online", ln.URL())
	return http.Serve(ln, http.HandlerFunc(handler))
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, %s\n", r.Header.Get("authenticated-user"))
}
