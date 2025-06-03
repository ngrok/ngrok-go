package legacy

import (
	"context"
	"log/slog"
)

// defaultLogger returns a no-op logger that discards all messages
func defaultLogger() *slog.Logger {
	// replace with 'slog.DiscardHandler' and delete the below struct once the package is go1.24
	return slog.New(discardHandler{})
}

type discardHandler struct{}

func (dh discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (dh discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (dh discardHandler) WithAttrs(attrs []slog.Attr) slog.Handler  { return dh }
func (dh discardHandler) WithGroup(name string) slog.Handler        { return dh }
