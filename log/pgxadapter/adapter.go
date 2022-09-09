package pgxadapter

import (
	"context"

	"github.com/jackc/pgx/v4"
	"github.com/ngrok/libngrok-go/log"
)

// Adapter for the pgx logging interface.
// Provided for extra compatibility without needing to reinvent the wheel.
type Logger struct {
	l pgx.Logger
}

var _ log.Logger = &Logger{}

func NewLogger(l pgx.Logger) *Logger {
	return &Logger{l}
}

func (l *Logger) Log(ctx context.Context, lvl int, msg string, data map[string]interface{}) {
	// Our log levels match the pgx levels, so we can simply cast it without all
	// of the switch/case shenanigans.
	l.l.Log(ctx, pgx.LogLevel(lvl), msg, data)
}
