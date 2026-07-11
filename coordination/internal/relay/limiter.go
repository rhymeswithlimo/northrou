package relay

import (
	"sync"
	"time"
)

// limiter is a small in-memory sliding-window rate limiter keyed by an
// arbitrary string. It is intentionally simple and process-local: the relay is
// abuse-limited, not a distributed quota system. Running more than one relay
// instance splits these counters, so the effective caps are per-instance (see
// the package doc). State is pruned lazily so memory stays bounded by the set
// of keys seen within the window.
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

// prune drops keys with no recent hits so the map does not grow without bound
// under a churn of distinct keys.
func (l *limiter) prune() {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := l.now().Add(-l.window)
	for k, ts := range l.hits {
		live := ts[:0]
		for _, t := range ts {
			if t.After(cutoff) {
				live = append(live, t)
			}
		}
		if len(live) == 0 {
			delete(l.hits, k)
		} else {
			l.hits[k] = live
		}
	}
}
