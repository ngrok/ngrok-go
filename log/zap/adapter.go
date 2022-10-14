// Package zap provides a logger that writes to a go.uber.org/zap.Logger and
// implements the github.com/ngrok/ngrok-go/log.Logger interface.
//
// Adapted from the github.com/jackc/pgx zap adapter.
package zap

import (
	"context"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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

type Logger struct {
	logger *zap.Logger
}

func NewLogger(logger *zap.Logger) *Logger {
	return &Logger{logger: logger.WithOptions(zap.AddCallerSkip(1))}
}

func (pl *Logger) Log(ctx context.Context, level LogLevel, msg string, data map[string]interface{}) {
	fields := make([]zapcore.Field, len(data))
	i := 0
	for k, v := range data {
		fields[i] = zap.Any(k, v)
		i++
	}

	switch level {
	case LogLevelTrace:
		pl.logger.Debug(msg, append(fields, zap.Any("LOG_LEVEL", level))...)
	case LogLevelDebug:
		pl.logger.Debug(msg, fields...)
	case LogLevelInfo:
		pl.logger.Info(msg, fields...)
	case LogLevelWarn:
		pl.logger.Warn(msg, fields...)
	case LogLevelError:
		pl.logger.Error(msg, fields...)
	default:
		pl.logger.Error(msg, append(fields, zap.Any("INVALID_LOG_LEVEL", level))...)
	}
}
