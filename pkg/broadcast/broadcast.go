// Package broadcast wakes every waiter at once: a waiter takes the
// channel from Wait, and the next Notify closes it.
package broadcast

import "sync"

// Signal is ready to use as its zero value and safe for concurrent use.
type Signal struct {
	mu sync.Mutex
	ch chan struct{}
}

// Wait returns a channel closed at the next Notify. Take it before
// reading the state it guards, so a change in between is not missed.
func (s *Signal) Wait() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ch == nil {
		s.ch = make(chan struct{})
	}
	return s.ch
}

// Notify wakes everyone waiting.
func (s *Signal) Notify() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ch != nil {
		close(s.ch)
		s.ch = nil
	}
}
