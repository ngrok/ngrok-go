package muxado

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
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
	err     error
	maxSize int
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

func (b *inboundBuffer) Read(p []byte) (n int, err error) {
	b.mu.Lock()
	for {
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
	b.mu.Unlock()
	b.cond.Broadcast()
}

func (b *inboundBuffer) SetDeadline(t time.Time) {
	// XXX: implement
	/*
		b.L.Lock()

		// set the deadline
		b.deadline = t

		// how long until the deadline
		delay := t.Sub(time.Now())

		if b.timer != nil {
			b.timer.Stop()
		}

		// after the delay, wake up waiters
		b.timer = time.AfterFunc(delay, func() {
			b.Broadcast()
		})

		b.L.Unlock()
	*/
}
