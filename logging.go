package ngrok

import (
	"context"
	"fmt"

	"github.com/inconshreveable/log15"
)

// The level of a log message.
type LogLevel = int

const (
	LogLevelTrace = 6
	LogLevelDebug = 5
	LogLevelInfo  = 4
	LogLevelWarn  = 3
	LogLevelError = 2
	LogLevelNone  = 1
)

// Logger defines a logging interface. It is identical to the log.Logger
// interface in [github.com/ngrok/libngrok-go/log.Logger]. It is duplicated here
// to avoid having to unconditionally import that submodule. Documentation lives
// in the [github.com/ngrok/libngrok-go/log.Logger] submodule, as well as
// adapters for other logging libraries.
// If you are implementing a logger, you should use the `log` submodule instead,
// as it also includes things like level formatting functions and doesn't
// require importing the full `ngrok` module.
type Logger interface {
	// Log a message at the given level with data key/value pairs. data may be
	// nil.
	Log(context context.Context, level LogLevel, msg string, data map[string]interface{})
}

type log15Handler struct {
	Logger
}

// The internals all use log15, so we need to convert the public logging
// interface to log15.
// If the provided Logger also implements the log15 interface, downcast and use
// that instead of wrapping again. This is the case for the Logger constructed
// by the log15adapter module.
// Otherwise, a new log15.Logger is constructed and the provided Logger used as
// its Handler.
func toLog15(l Logger) log15.Logger {
	if logger, ok := l.(log15.Logger); ok {
		return logger
	}

	logger := log15.New()
	logger.SetHandler(&log15Handler{l})

	return logger
}

func (l *log15Handler) Log(r *log15.Record) error {
	lvl := LogLevelNone
	switch r.Lvl {
	case log15.LvlCrit:
		lvl = LogLevelError
	case log15.LvlError:
		lvl = LogLevelError
	case log15.LvlWarn:
		lvl = LogLevelWarn
	case log15.LvlInfo:
		lvl = LogLevelInfo
	case log15.LvlDebug:
		lvl = LogLevelDebug
	case log15.LvlDebug + 1:
		// Also support trace, if someone happens to hack
		// it in.
		lvl = LogLevelTrace
	}

	data := make(map[string]interface{}, len(r.Ctx)/2)
	for i := 0; i < len(r.Ctx); i += 2 {
		var (
			k  string
			ok bool
			v  interface{}
		)
		// The default upstream log15 formatter chooses to treat non-strings as
		// errors. We'll be a bit nicer and Sprint it instead if we find one.
		k, ok = r.Ctx[i].(string)
		if !ok {
			k = fmt.Sprint(r.Ctx[i])
		}
		// I think log15 guarantees an even number of context values, but just
		// in case.
		if len(r.Ctx) > i+1 {
			v = r.Ctx[i+1]
		} else {
			v = "MISSING_VALUE"
		}
		data[k] = v
	}

	l.Logger.Log(context.Background(), lvl, r.Msg, data)
	return nil
}
