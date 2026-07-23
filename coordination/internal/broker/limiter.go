package broker

import (
	"sync"
	"time"
)

// limiter is a small in-memory sliding-window rate limiter keyed by an arbitrary
// string. It bounds abuse (here, connection-code guessing via connect, which is
// a code-validity oracle: it answers "paired" for a real code and "error"
// otherwise). It is process-local; running more than one coordinator splits the
// counters, so the effective caps are per-instance. State is pruned lazily so
// memory stays bounded by the set of keys seen within the window. Copied from
// the former relay package, which is being removed.
type limiter struct {
	mu     sync.Mutex
	window time.Duration
	max    int
	hits   map[string][]time.Time
	now    func() time.Time // injectable for tests
}

func newLimiter(window time.Duration, max int) *limiter {
	return &limiter{
		window: window,
		max:    max,
		hits:   make(map[string][]time.Time),
		now:    time.Now,
	}
}

// allow records an attempt for key and reports whether it is within the cap. A
// rejected attempt is NOT recorded, so a caller that keeps hammering a maxed-out
// key does not extend its own lockout.
func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := l.now().Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, l.now())
	return true
}
