package identity

import (
	"sync"
	"time"
)

// limiter counts failed password attempts per key (an email address) in
// a sliding window. It is per process, which is enough for one API
// instance; a second instance would halve its strength, not void it.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	fails  map[string][]time.Time
}

func newLimiter(maxFails int, window time.Duration) *limiter {
	return &limiter{max: maxFails, window: window, fails: map[string][]time.Time{}}
}

// attempt counts an attempt for key at now and reports whether it may
// go ahead: false once key has used up its attempts. It counts before
// the password is checked, so parallel guesses cannot all slip in under
// the limit; a successful attempt clears the count (reset).
func (l *limiter) attempt(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.fails) > 10_000 {
		for k := range l.fails {
			l.recent(k, now)
		}
	}
	recent := l.recent(key, now)
	if len(recent) >= l.max {
		return false
	}
	l.fails[key] = append(recent, now)
	return true
}

func (l *limiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}

// recent drops attempts older than the window and returns the rest.
func (l *limiter) recent(key string, now time.Time) []time.Time {
	ts := l.fails[key]
	i := 0
	for i < len(ts) && now.Sub(ts[i]) >= l.window {
		i++
	}
	ts = ts[i:]
	if len(ts) == 0 {
		delete(l.fails, key)
		return nil
	}
	l.fails[key] = ts
	return ts
}
