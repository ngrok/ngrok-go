// Package slog provides a logger that writes
// to a log/slog.Logger and implements the
// golang.ngrok.com/ngrok/log.Logger interface.
package slog

import (
	"context"

	"log/slog"
)

type LogLevel = int

// Log level constants matching the ones in golang.ngrok.com/ngrok/log
const (
	LogLevelTrace = 6
	LogLevelDebug = 5
	LogLevelInfo  = 4
	LogLevelWarn  = 3
	LogLevelError = 2
	LogLevelNone  = 1
)

// Wrapper for a slog.Logger to add the ngrok logging interface.
// Also exposes the slog.Logger interface directly so that it can be downcast
// to the slog.Logger.
type Logger struct {
	inner *slog.Logger
}

func NewLogger(l *slog.Logger) *Logger {
	return &Logger{l}
}

func (l *Logger) Log(ctx context.Context, level LogLevel, msg string, data map[string]interface{}) {
	logArgs := make([]interface{}, 0, len(data))
	for k, v := range data {
		logArgs = append(logArgs, k, v)
	}

	switch level {
	case LogLevelTrace:
		l.inner.Debug(msg, append(logArgs, "LOG_LEVEL", level)...)
	case LogLevelDebug:
		l.inner.Debug(msg, logArgs...)
	case LogLevelInfo:
		l.inner.Info(msg, logArgs...)
	case LogLevelWarn:
		l.inner.Warn(msg, logArgs...)
	case LogLevelError:
		l.inner.Error(msg, logArgs...)
	default:
		l.inner.Error(msg, append(logArgs, "INVALID_LOG_LEVEL", level)...)
	}
}
