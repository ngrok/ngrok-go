package testutil

import (
	"sync"
	"testing"
	"time"
)

// SyncPoint coordinates test execution points
type SyncPoint struct {
	ch     chan struct{}
	called bool
	mu     sync.Mutex
}

// NewSyncPoint creates a new synchronization point
func NewSyncPoint() *SyncPoint {
	return &SyncPoint{
		ch: make(chan struct{}),
	}
}

// Signal marks the sync point as reached
func (s *SyncPoint) Signal() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.called {
		close(s.ch)
		s.called = true
	}
}

// Wait blocks until the sync point is signaled or times out
func (s *SyncPoint) Wait(t testing.TB) {
	t.Helper()
	select {
	case <-s.ch:
		return
	case <-time.After(5 * time.Second): // Safety timeout
		t.Fatal("timeout waiting for sync point")
	}
}

// WaitTimeout waits for the sync point with a custom timeout
func (s *SyncPoint) WaitTimeout(t testing.TB, timeout time.Duration) bool {
	t.Helper()
	select {
	case <-s.ch:
		return true
	case <-time.After(timeout):
		return false
	}
}

// WaitGroup is a wrapper around sync.WaitGroup with timeouts
type WaitGroup struct {
	wg      sync.WaitGroup
	done    chan struct{}
	started bool
}

// NewWaitGroup creates a new wait group with timeout capability
func NewWaitGroup() *WaitGroup {
	return &WaitGroup{
		done: make(chan struct{}),
	}
}

// Add adds delta to the WaitGroup counter
func (w *WaitGroup) Add(delta int) {
	w.wg.Add(delta)
	if !w.started {
		w.started = true
		go func() {
			w.wg.Wait()
			close(w.done)
		}()
	}
}

// Done decrements the WaitGroup counter
func (w *WaitGroup) Done() {
	w.wg.Done()
}

// Wait waits for the WaitGroup counter to be zero
func (w *WaitGroup) Wait(t testing.TB) {
	t.Helper()
	select {
	case <-w.done:
		return
	case <-time.After(10 * time.Second): // Safety timeout
		t.Fatal("timeout waiting for wait group")
	}
}
