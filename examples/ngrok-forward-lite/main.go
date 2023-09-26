// Naïve ngrok agent implementation.
// Sets up a single tunnel and forwards it to another service.

package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"

	"golang.ngrok.com/ngrok"
	"golang.ngrok.com/ngrok/config"
	ngrok_log "golang.ngrok.com/ngrok/log"
)

func usage(bin string) {
	log.Fatalf("Usage: %s <url>", bin)
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

var l *logger = &logger{
	lvl: ngrok_log.LogLevelDebug,
}

func main() {
	if len(os.Args) != 2 {
		usage(os.Args[0])
	}
	backend := os.Args[1]
	if !strings.Contains(backend, "://") {
		backend = fmt.Sprintf("tcp://%s", backend)
	}

	backendUrl, err := url.Parse(backend)
	if err != nil {
		usage(os.Args[0])
	}

	if err := run(context.Background(), backendUrl); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, backend *url.URL) error {
	fwd, err := ngrok.ListenAndForward(ctx,
		backend,
		config.HTTPEndpoint(),
		ngrok.WithAuthtokenFromEnv(),
		ngrok.WithLogger(&logger{lvl: ngrok_log.LogLevelDebug}),
	)
	if err != nil {
		return err
	}

	l.Log(ctx, ngrok_log.LogLevelInfo, "tunnel created", map[string]any{
		"url": fwd.URL(),
	})

	return fwd.Wait()
}
