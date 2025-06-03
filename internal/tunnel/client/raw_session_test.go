package client

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"golang.ngrok.com/muxado/v2"
)

type dummyStream struct{}

func (d *dummyStream) Read(bs []byte) (int, error)  { return 0, nil }
func (d *dummyStream) Write(bs []byte) (int, error) { return 0, nil }
func (d *dummyStream) Close() error                 { return nil }

func TestRawSessionDoubleClose(t *testing.T) {
	r := NewRawSession(slog.Default(), muxado.Client(&dummyStream{}, nil), nil, nil)

	// Verify that closing the session twice doesn't cause a panic
	r.Close()
	r.Close()
}

func TestHeartbeatTimeout(t *testing.T) {
	r := NewRawSession(slog.Default(), muxado.Client(&dummyStream{}, nil), nil, nil)
	// Make sure we don't deadlock
	r.(*rawSession).onHeartbeat(1, true)
}

func TestRawSessionCloseRace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*1)
	defer cancel()

	// Since this is a race condition, run the test as many times as we can
	// within the timebox to see if we can hit it.
testloop:
	for {
		select {
		case <-ctx.Done():
			break testloop
		default:
		}

		ctx, cancel := context.WithCancel(ctx)
		logger := slog.Default()
		r := NewRawSession(logger, muxado.Client(&dummyStream{}, nil), nil, nil)

		wg := sync.WaitGroup{}
		wg.Add(1)

		// Call onHeartbeat as fast as we can in the background.
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				r.(*rawSession).onHeartbeat(time.Millisecond*1, false)
			}
		}()

		// Verify that closing the session while a heartbeat is in flight won't
		// cause a panic
		r.Close()

		cancel()

		// Wait till the heartbeat goroutine exists to make sure we capture the
		// panic and it doesn't occur after the test completes.
		wg.Wait()
	}
}
