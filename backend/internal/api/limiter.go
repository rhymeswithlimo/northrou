package api

import (
	"sync"
	"time"
)

// limiter is a small in-memory sliding-window rate limiter keyed by an arbitrary
// string. It bounds abuse (here, connection-code guessing on /api/auth/pair); it
// is not a distributed quota system. State is pruned lazily so memory stays
// bounded by the set of keys seen within the window.
//
// Note: remote pair attempts all arrive over the WebRTC tunnel with no client IP
// the box can see (the RemoteAddr is a synthetic "webrtc:0"), so per-IP keying is
// not possible here and the pair limiter is global. Per-IP throttling of the
// pairing hop happens upstream at the coordinator's connect handler, which does
// see real client IPs. Copied from coordination/internal/relay so the two
// modules do not share a package.
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
