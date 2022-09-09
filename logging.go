package libngrok

import (
	"context"
	"fmt"

	"github.com/inconshreveable/log15"
	"github.com/ngrok/libngrok-go/log"
)

type log15Handler struct {
	log.Logger
}

// The internals all use log15, so we need to convert the public logging
// interface to log15.
// If the provided Logger also implements the log15 interface, downcast and use
// that instead of wrapping again. This is the case for the Logger constructed
// by the log15adapter module.
// Otherwise, a new log15.Logger is constructed and the provided Logger used as
// its Handler.
func toLog15(l log.Logger) log15.Logger {
	if logger, ok := l.(log15.Logger); ok {
		return logger
	}

	logger := log15.New()
	logger.SetHandler(&log15Handler{l})

	return logger
}

func (l *log15Handler) Log(r *log15.Record) error {
	lvl := log.LogLevelNone
	switch r.Lvl {
	case log15.LvlCrit:
		lvl = log.LogLevelError
	case log15.LvlError:
		lvl = log.LogLevelError
	case log15.LvlWarn:
		lvl = log.LogLevelWarn
	case log15.LvlInfo:
		lvl = log.LogLevelInfo
	case log15.LvlDebug:
		lvl = log.LogLevelDebug
	case log15.LvlDebug + 1:
		// Also support trace, if someone happens to hack
		// it in.
		lvl = log.LogLevelTrace
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
