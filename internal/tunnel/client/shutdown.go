package client

import "sync"

// facilitates controlled shutdowns of resources
type shutdown struct {
	shutting bool
	sync.RWMutex
	once sync.Once
}

// Do runs the given function and returns true, unless a shutdown has happened
// or is in progress, in which case it returns false and does not run the
// function.
func (s *shutdown) Do(fn func()) bool {
	s.RLock()
	defer s.RUnlock()
	if s.shutting {
		return false
	}
	fn()
	return true
}

// Shut runs the given function which contains the shutdown logic. It guarantees
// that function will only ever be run once. It further guarantees that when
// that function runs, no calls to Do will be in progress or ever succeed again.
func (s *shutdown) Shut(fn func()) {
	s.Lock()
	s.shutting = true
	s.Unlock()
	s.once.Do(fn)
}
