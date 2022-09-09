package log15adapter

import (
	"context"

	"github.com/inconshreveable/log15"
	"github.com/ngrok/libngrok-go/log"
)

// Wrapper for a log15.Logger to add the libngrok logging interface.
// Also exposes the log15.Logger interface directly so that it can be downcast
// to the log15.Logger.
type Logger struct {
	log15.Logger
}

func NewLogger(l log15.Logger) *Logger {
	return &Logger{l}
}

var _ log.Logger = &Logger{}

func (l *Logger) Log(ctx context.Context, level log.LogLevel, msg string, data map[string]interface{}) {
	logArgs := make([]interface{}, 0, len(data))
	for k, v := range data {
		logArgs = append(logArgs, k, v)
	}

	switch level {
	case log.LogLevelTrace:
		l.Debug(msg, append(logArgs, "LOG_LEVEL", level)...)
	case log.LogLevelDebug:
		l.Debug(msg, logArgs...)
	case log.LogLevelInfo:
		l.Info(msg, logArgs...)
	case log.LogLevelWarn:
		l.Warn(msg, logArgs...)
	case log.LogLevelError:
		l.Error(msg, logArgs...)
	default:
		l.Error(msg, append(logArgs, "INVALID_LOG_LEVEL", level)...)
	}
}
