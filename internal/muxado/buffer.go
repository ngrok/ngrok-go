package muxado

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"sync"
	"time"
)

var (
	bufferFull   = errors.New("buffer is full")
	bufferClosed = errors.New("buffer closed previously")
)

type buffer interface {
	Read([]byte) (int, error)
	ReadFrom(io.Reader) (int64, error)
	SetError(error)
	SetDeadline(time.Time)
}

type inboundBuffer struct {
	cond sync.Cond
	mu   sync.Mutex
	bytes.Buffer
	err      error
	maxSize  int
	deadline time.Time
	timer    *time.Timer
}

func (b *inboundBuffer) Init(maxSize int) {
	b.cond.L = &b.mu
	b.maxSize = maxSize
}

func (b *inboundBuffer) ReadFrom(rd io.Reader) (n int64, err error) {
	var n64 int64
	b.mu.Lock()
	if b.err != nil {
		if _, err = ioutil.ReadAll(rd); err == nil {
			err = bufferClosed
		}
		goto DONE
	}

	n64, err = b.Buffer.ReadFrom(rd)
	n += n64
	if b.Buffer.Len() > b.maxSize {
		err = bufferFull
		b.err = bufferFull
	}

	b.cond.Broadcast()
DONE:
	b.mu.Unlock()
	return n, err
}

// Notify readers that the deadline has arrived.
func (b *inboundBuffer) notifyDeadline() {
	// It's important that the mutex is locked for this. It ensures that an
	// in-flight timer can't broadcast before a reader gets to its condvar.Wait().
	b.mu.Lock()
	b.cond.Broadcast()
	b.mu.Unlock()
}

// Start or reset the timer.
// Must be called when the mutex is locked.
func (b *inboundBuffer) startTimerLocked(timeout time.Duration) {
	if b.timer == nil {
		b.timer = time.AfterFunc(timeout, b.notifyDeadline)
	} else {
		b.timer.Reset(timeout)
	}
}

// Stops a timer, if one is set.
// Must be called while the mutex is locked.
func (b *inboundBuffer) stopTimerLocked() {
	if b.timer != nil {
		b.timer.Stop()
	}
}

func (b *inboundBuffer) Read(p []byte) (n int, err error) {
	b.mu.Lock()
	for {
		// If the deadline is set, we need to take it into account
		if !b.deadline.IsZero() {
			// If the deadline is in the past, bail out.
			// SetDeadline will ensure that we get woken back up if it expires.
			if time.Until(b.deadline) < 0 {
				n = 0
				err = os.ErrDeadlineExceeded
				break
			}
		}

		if b.Len() != 0 {
			n, err = b.Buffer.Read(p)
			break
		}
		if b.err != nil {
			err = b.err
			break
		}

		b.cond.Wait()
	}
	b.mu.Unlock()
	return
}

func (b *inboundBuffer) SetError(err error) {
	b.mu.Lock()
	b.err = err
	b.cond.Broadcast()
	b.mu.Unlock()
}

func (b *inboundBuffer) SetDeadline(t time.Time) {
	b.mu.Lock()

	// Set the deadline and notify any readers that they need to take heed.
	// They'll figure out all of the timer management for us.
	b.deadline = t
	if timeout := time.Until(t); timeout > 0 {
		b.startTimerLocked(timeout)
	} else {
		b.stopTimerLocked()
	}

	b.cond.Broadcast()

	b.mu.Unlock()
}

func (b *inboundBuffer) Close() error {
	b.mu.Lock()
	b.stopTimerLocked()
	b.err = io.EOF
	b.cond.Broadcast()
	b.mu.Unlock()
	return nil
}
