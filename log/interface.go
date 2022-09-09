package log

import (
	"context"
	"fmt"
)

type LogLevel = int

type ErrInvalidLogLevel struct {
	Level any
}

func (e ErrInvalidLogLevel) Error() string {
	return fmt.Sprintf("invalid log level: %v", e.Level)
}

const (
	LogLevelTrace = 6
	LogLevelDebug = 5
	LogLevelInfo  = 4
	LogLevelWarn  = 3
	LogLevelError = 2
	LogLevelNone  = 1
)

// Logging interface, heavily inspired by github.com/jackc/pgx's logger.
// The primary difference is that `LogLevel` is a type alias rather than a
// newtype. This makes it easier for other libraries to support the interface (in theory),
// as they don't need to depend on this package directly.
//
// Adapters are provided for pgx and log15.
type Logger interface {
	// Log a message at the given level with data key/value pairs. data may be nil.
	Log(context context.Context, level LogLevel, msg string, data map[string]interface{})
}

func StringFromLogLevel(lvl LogLevel) (string, error) {
	switch lvl {
	case LogLevelTrace:
		return "trace", nil
	case LogLevelDebug:
		return "debug", nil
	case LogLevelInfo:
		return "info", nil
	case LogLevelWarn:
		return "warn", nil
	case LogLevelError:
		return "error", nil
	case LogLevelNone:
		return "none", nil
	default:
		return "invalid", ErrInvalidLogLevel{lvl}
	}
}

func LogLevelFromString(s string) (LogLevel, error) {
	switch s {
	case "trace":
		return LogLevelTrace, nil
	case "debug":
		return LogLevelDebug, nil
	case "info":
		return LogLevelInfo, nil
	case "warn":
		return LogLevelWarn, nil
	case "error":
		return LogLevelError, nil
	case "none":
		return LogLevelNone, nil
	default:
		return 0, ErrInvalidLogLevel{s}
	}
}
