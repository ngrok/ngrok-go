// Package log15 provides a logger that writes to a
// github.com/inconshreveable/log15.Logger and implements the
// github.com/ngrok/ngrok-go/log.Logger interface.
//
// Adapted from the github.com/jackc/pgx log15 adapter.
package log15

import (
	"context"

	"github.com/inconshreveable/log15"
)

type LogLevel = int

// Log level constants matching the ones in github.com/ngrok/ngrok-go/log
const (
	LogLevelTrace = 6
	LogLevelDebug = 5
	LogLevelInfo  = 4
	LogLevelWarn  = 3
	LogLevelError = 2
	LogLevelNone  = 1
)

// Wrapper for a log15.Logger to add the ngrok logging interface.
// Also exposes the log15.Logger interface directly so that it can be downcast
// to the log15.Logger.
type Logger struct {
	log15.Logger
}

func NewLogger(l log15.Logger) *Logger {
	return &Logger{l}
}

func (l *Logger) Log(ctx context.Context, level LogLevel, msg string, data map[string]interface{}) {
	logArgs := make([]interface{}, 0, len(data))
	for k, v := range data {
		logArgs = append(logArgs, k, v)
	}

	switch level {
	case LogLevelTrace:
		l.Debug(msg, append(logArgs, "LOG_LEVEL", level)...)
	case LogLevelDebug:
		l.Debug(msg, logArgs...)
	case LogLevelInfo:
		l.Info(msg, logArgs...)
	case LogLevelWarn:
		l.Warn(msg, logArgs...)
	case LogLevelError:
		l.Error(msg, logArgs...)
	default:
		l.Error(msg, append(logArgs, "INVALID_LOG_LEVEL", level)...)
	}
}
