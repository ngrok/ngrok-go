// Setting up a custom logger.
// Takes the desired log level as the first CLI argument.

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/ngrok/ngrok-go"
	"github.com/ngrok/ngrok-go/config"
	ngrok_log "github.com/ngrok/ngrok-go/log"
)

func usage(bin string) {
	log.Fatalf("Usage: %s <log level>", bin)
}

func main() {
	if len(os.Args) != 2 {
		usage(os.Args[0])
	}
	if err := run(context.Background(), os.Args[1]); err != nil {
		log.Fatal(err)
	}
}

// Simple logger that forwards to the Go standard logger.
type logger struct {
	lvl ngrok_log.LogLevel
}

func (l *logger) Log(ctx context.Context, lvl ngrok_log.LogLevel, msg string, data map[string]interface{}) {
	if lvl > l.lvl {
		return
	}
	lvlName, _ := ngrok_log.StringFromLogLevel(lvl)
	log.Printf("[%s] %s %v", lvlName, msg, data)
}

func run(ctx context.Context, lvlName string) error {
	lvl, err := ngrok_log.LogLevelFromString(lvlName)
	if err != nil {
		return err
	}

	tun, err := ngrok.StartTunnel(ctx,
		config.HTTPEndpoint(),
		ngrok.WithAuthtokenFromEnv(),
		ngrok.WithLogger(&logger{lvl}),
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
