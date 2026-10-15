package broadcast_test

import (
	"testing"

	"kurgu/pkg/broadcast"
)

func TestSignal(t *testing.T) {
	var s broadcast.Signal
	s.Notify() // nobody waits: a no-op
	a, b := s.Wait(), s.Wait()
	if a != b {
		t.Fatal("waiters before one Notify share a channel")
	}
	select {
	case <-a:
		t.Fatal("closed before Notify")
	default:
	}
	s.Notify()
	for _, ch := range []<-chan struct{}{a, b} {
		select {
		case <-ch:
		default:
			t.Fatal("Notify did not wake a waiter")
		}
	}
	select {
	case <-s.Wait():
		t.Fatal("a new wait is already closed")
	default:
	}
}
