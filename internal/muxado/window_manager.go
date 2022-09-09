package muxado

import (
	"sync"
)

type windowManager interface {
	Increment(int)
	Decrement(int) (int, error)
	SetError(error)
}

type condWindow struct {
	val     int
	maxSize int
	err     error
	sync.Cond
	sync.Mutex
}

func newCondWindow(initialSize int) *condWindow {
	w := new(condWindow)
	w.Init(initialSize)
	return w
}

func (w *condWindow) Init(initialSize int) {
	w.val = initialSize
	w.maxSize = initialSize
	w.Cond.L = &w.Mutex
}

func (w *condWindow) Increment(inc int) {
	w.L.Lock()
	w.val += inc
	w.Broadcast()
	w.L.Unlock()
}

func (w *condWindow) SetError(err error) {
	w.L.Lock()
	w.err = err
	w.Broadcast()
	w.L.Unlock()
}

func (w *condWindow) Decrement(dec int) (ret int, err error) {
	if dec == 0 {
		return
	}

	w.L.Lock()
	for {
		if w.err != nil {
			err = w.err
			break
		}

		if w.val > 0 {
			if dec > w.val {
				ret = w.val
				w.val = 0
				break
			} else {
				ret = dec
				w.val -= dec
				break
			}
		} else {
			w.Wait()
		}
	}
	w.L.Unlock()
	return
}
