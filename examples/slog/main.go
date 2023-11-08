// Setting up a slog logger.
// Takes the desired log level as the first CLI argument.

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"golang.org/x/exp/slog"

	"golang.ngrok.com/ngrok"
	"golang.ngrok.com/ngrok/config"
	slogadapter "golang.ngrok.com/ngrok/log/slog"
)

func usage(bin string) {
	fmt.Printf("Usage: %s <log level>\n", bin)
	os.Exit(1)
}

func main() {
	if len(os.Args) != 2 {
		usage(os.Args[0])
	}
	if err := run(context.Background(), os.Args[1]); err != nil {
		slog.Error("exited with error", err)
		os.Exit(1)
	}
}

var programLevel = new(slog.LevelVar) // Info by default

func run(ctx context.Context, lvlName string) error {
	switch strings.ToUpper(lvlName) {
	case "DEBUG":
		programLevel.Set(slog.LevelDebug)
	case "INFO":
		programLevel.Set(slog.LevelInfo)
	case "WARN":
		programLevel.Set(slog.LevelWarn)
	case "ERROR":
		programLevel.Set(slog.LevelError)
	default:
		return fmt.Errorf("invalid log level: %s", lvlName)
	}
	h := slog.HandlerOptions{Level: programLevel}.NewTextHandler(os.Stdout)
	slog.SetDefault(slog.New(h))

	ln, err := ngrok.Listen(ctx,
		config.HTTPEndpoint(),
		ngrok.WithAuthtokenFromEnv(),
		ngrok.WithLogger(slogadapter.NewLogger(slog.Default())),
	)
	if err != nil {
		return err
	}

	slog.Info("Ingress established", "url", ln.URL())

	return http.Serve(ln, http.HandlerFunc(handler))
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello from ngrok-go!")
}
