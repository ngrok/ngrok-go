// Naïve ngrok agent implementation.
// Sets up a single tunnel and connects to an arbitrary HTTP server.

package main

import (
	"context"
	"log"
	"net/http"

	"golang.ngrok.com/ngrok"
	"golang.ngrok.com/ngrok/config"
	ngrok_log "golang.ngrok.com/ngrok/log"
)

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
	// Spin up a simple HTTP server
	server := &http.Server{}
	server.ListenAndServe()

	// Serve with tunnel backend
	if err := run(context.Background(), server); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, server *http.Server) error {
	tunnel, err := ngrok.ListenAndServeHTTP(ctx,
		server,
		config.HTTPEndpoint(),
		ngrok.WithAuthtokenFromEnv(),
		ngrok.WithLogger(&logger{lvl: ngrok_log.LogLevelDebug}),
	)

	if err == nil {
		l.Log(ctx, ngrok_log.LogLevelInfo, "tunnel created", map[string]any{
			"url": tunnel.URL(),
		})
	}

	return err
}
