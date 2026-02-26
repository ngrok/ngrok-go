package httpx

import (
	"errors"
	"sync"
)

var ErrShutdown = errors.New("shutdown")

// Shutdown allows a concurrently-accessed resource or piece of functionality
// to terminate gracefully. It is similar to a semaphore.
//
// Shutdown allows a caller to track how many active operations are concurrently
// in-flight. When the Shutdown() method is called, the following guarantees are
// made:
//
// All future operations that a caller tries to initiate via calls to StartOp() or
// Do() will be rejected.
//
// Shutdown() will block until all in-flight operations are complete.
//
// The zero-value of a Shutdown is safe to use.
// A Shutdown object contains a sync.Mutex so it is *not* safe to copy.
type Shutdown struct {
	mu       sync.Mutex
	c        sync.Cond
	count    int
	shut     bool
	initOnce sync.Once

	ch chan struct{} // channel is closed when shutdown is complete
}

// C returns a channel that is closed when the shutdown is complete
func (s *Shutdown) C() <-chan struct{} {
	s.maybeInit()
	return s.ch
}

func (s *Shutdown) maybeInit() {
	s.initOnce.Do(func() {
		s.ch = make(chan struct{})
		s.c.L = &s.mu
	})
}

// Start a new operation.
func (s *Shutdown) StartOp() bool {
	s.maybeInit()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.shut {
		return false
	} else {
		s.count += 1
		return true
	}
}

// Finish an operation.
func (s *Shutdown) FinishOp() {
	s.maybeInit()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.count -= 1
	if s.count == 0 {
		s.c.Broadcast()
	}
}

// Do runs the given function and returns true, unless a shutdown has happened
// or is in progress, in which case it returns false and does not run the
// function.
func (s *Shutdown) Do(fn func()) bool {
	if !s.StartOp() {
		return false
	}
	defer s.FinishOp()
	fn()
	return true
}

// Shutdown begins shutting down this group of tasks. After Shutdown returns,
// further calls to Do() will not execute their supplied function and will
// return false.
// Shutdown returns true if this is the first call to Shutdown(). Additional
// calls will return false.
// To wait on shutdown to complete fully, use 'C()'
func (s *Shutdown) Shutdown() bool {
	s.maybeInit()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shut {
		return false
	}
	s.shut = true
	go s.drainTasks()
	return true
}

// drainTasks is called for the first shutdown call and is responsible for
// closing 's.ch' once all tasks are done.
func (s *Shutdown) drainTasks() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for s.count != 0 {
		s.c.Wait()
	}
	close(s.ch)
}

// Err checks if this instance is already shutdown
//   - if shutdown, it returns ErrShutdown
//   - if alive/not shutdown, it returns nil
func (s *Shutdown) Err() error {
	s.maybeInit()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.shut {
		return ErrShutdown
	}
	return nil
}
