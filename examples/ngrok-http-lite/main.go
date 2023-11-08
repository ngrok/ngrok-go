// Naïve ngrok agent implementation.
// Sets up a single listener and connects to an arbitrary HTTP server.

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

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
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Hello from ngrok-go!")
	})}

	// Serve with listener backend
	if err := run(context.Background(), server); err != nil {
		log.Fatal(err)
	}

	// Sleep main thread
	for {
		time.Sleep(5 * time.Second)
	}
}

func run(ctx context.Context, server *http.Server) error {
	ln, err := ngrok.ListenAndServeHTTP(ctx,
		server,
		config.HTTPEndpoint(),
		ngrok.WithAuthtokenFromEnv(),
		ngrok.WithLogger(&logger{lvl: ngrok_log.LogLevelDebug}),
	)

	if err == nil {
		l.Log(ctx, ngrok_log.LogLevelInfo, "ingress established", map[string]any{
			"url": ln.URL(),
		})
	}

	return err
}
