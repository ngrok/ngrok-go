// Package testcontext provides a function to obtain a [context.Context] in a test.
package testcontext

import (
	"context"
	"testing"
	"time"
)

// ForTB returns a [context.Context] that is canceled
// just before Cleanup-registered functions are called
// or shortly before the test deadline,
// whichever comes first.
func ForTB(tb testing.TB) context.Context {
	ctx := tb.Context()
	deadline, ok := tbDeadline(tb)
	if !ok {
		return ctx
	}
	ctx, cancel := context.WithDeadline(ctx, deadline.Add(-10*time.Second))
	tb.Cleanup(cancel)
	return ctx
}

func tbDeadline(tb testing.TB) (deadline time.Time, ok bool) {
	d, ok := tb.(deadliner)
	if !ok {
		return time.Time{}, false
	}
	return d.Deadline()
}

type deadliner interface {
	Deadline() (deadline time.Time, ok bool)
}

var _ deadliner = (*testing.T)(nil)
