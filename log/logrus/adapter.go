// Package logrus provides a logger that writes to a
// github.com/sirupsen/logrus.Logger and implements the
// github.com/ngrok/ngrok-go/log.Logger interface.
//
// Adapted from the github.com/jackc/pgx logrus adapter.
package logrus

import (
	"context"

	"github.com/sirupsen/logrus"
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
	l logrus.FieldLogger
}

func NewLogger(l logrus.FieldLogger) *Logger {
	return &Logger{l: l}
}

func (l *Logger) Log(ctx context.Context, level LogLevel, msg string, data map[string]interface{}) {
	var logger logrus.FieldLogger
	if data != nil {
		logger = l.l.WithFields(data)
	} else {
		logger = l.l
	}

	switch level {
	case LogLevelTrace:
		logger.WithField("LOG_LEVEL", level).Debug(msg)
	case LogLevelDebug:
		logger.Debug(msg)
	case LogLevelInfo:
		logger.Info(msg)
	case LogLevelWarn:
		logger.Warn(msg)
	case LogLevelError:
		logger.Error(msg)
	default:
		logger.WithField("INVALID_LOG_LEVEL", level).Error(msg)
	}
}
