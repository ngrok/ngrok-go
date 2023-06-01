package muxado

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"os"
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
	maxSize int
	lock    chan chan struct{}
	bytes.Buffer
	err      error
	deadline time.Time
	timer    *time.Timer
}

func (b *inboundBuffer) Init(maxSize int) {
	b.maxSize = maxSize
	b.lock = make(chan chan struct{}, 1)
	b.lock <- make(chan struct{})
}

func (b *inboundBuffer) ReadFrom(rd io.Reader) (n int64, err error) {
	var n64 int64
	cond := <-b.lock

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

	close(cond)
	cond = make(chan struct{})

DONE:
	b.lock <- cond
	return n, err
}

func (b *inboundBuffer) Read(p []byte) (n int, err error) {
	cond := <-b.lock
	for {
		if !b.deadline.IsZero() && time.Now().After(b.deadline) {
			err = os.ErrDeadlineExceeded
			break
		}

		if b.Len() != 0 {
			n, err = b.Buffer.Read(p)
			break
		}
		if b.err != nil {
			err = b.err
			break
		}

		b.lock <- cond
		<-cond
		cond = <-b.lock
	}
	b.lock <- cond
	return
}

func (b *inboundBuffer) SetError(err error) {
	cond := <-b.lock
	b.err = err
	close(cond)
	b.lock <- make(chan struct{})
}

func (b *inboundBuffer) deadlineReached() {
	cond := <-b.lock
	close(cond)
	b.lock <- make(chan struct{})
}

func (b *inboundBuffer) SetDeadline(t time.Time) {
	cond := <-b.lock

	close(cond)
	if b.timer != nil {
		b.timer.Stop()
	}

	b.deadline = t
	if u := t.Sub(time.Now()); u > 0 {
		b.timer = time.AfterFunc(u, b.deadlineReached)
	}

	b.lock <- make(chan struct{})
}
