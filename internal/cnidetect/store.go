package cnidetect

import "sync"

// Store holds the result of a CNI detection run.
// It is set once at operator startup and then read by controllers.
type Store struct {
	mu     sync.RWMutex
	result *Result
}

// Set stores the detection result. Safe to call from any goroutine.
func (s *Store) Set(r Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r2 := r
	s.result = &r2
}

// Result returns the stored detection result and true, or zero value + false
// if detection has not yet completed.
func (s *Store) Result() (Result, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.result == nil {
		return Result{}, false
	}
	return *s.result, true
}
