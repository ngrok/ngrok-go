package legacy

import (
	"github.com/inconshreveable/log15/v3"
	"log/slog"
)

// SlogToLog15 converts a slog.Logger to a log15.Logger for use with the legacy package
func SlogToLog15(slogger *slog.Logger) log15.Logger {
	logger := log15.New()
	logger.SetHandler(&slogHandler{logger: slogger})
	return logger
}

// slogHandler implements log15.Handler interface to adapt slog.Logger
type slogHandler struct {
	logger *slog.Logger
}

// Log implements log15.Handler interface
func (h *slogHandler) Log(r log15.Record) error {
	// Convert log15 context to slog attributes
	attrs := make([]any, 0, len(r.Ctx))
	for i := 0; i < len(r.Ctx); i += 2 {
		if i+1 < len(r.Ctx) {
			key, ok := r.Ctx[i].(string)
			if !ok {
				key = "unknown_key"
			}
			attrs = append(attrs, key, r.Ctx[i+1])
		}
	}

	// Map log15 levels to slog levels
	switch r.Lvl {
	case log15.LvlCrit, log15.LvlError:
		h.logger.Error(r.Msg, attrs...)
	case log15.LvlWarn:
		h.logger.Warn(r.Msg, attrs...)
	case log15.LvlInfo:
		h.logger.Info(r.Msg, attrs...)
	case log15.LvlDebug, log15.LvlDebug + 1: // Handle trace level too
		h.logger.Debug(r.Msg, attrs...)
	default:
		h.logger.Info(r.Msg, append(attrs, "original_level", r.Lvl)...)
	}

	return nil
}

// defaultLogger returns a no-op logger that discards all messages
func defaultLogger() log15.Logger {
	logger := log15.New()
	logger.SetHandler(log15.DiscardHandler())
	return logger
}
