package broker

import (
	"testing"
	"time"
)

func TestLimiterCapsWithinWindow(t *testing.T) {
	now := time.Now()
	l := newLimiter(time.Minute, 3)
	l.now = func() time.Time { return now }

	// First three attempts for a key are allowed; the fourth is not.
	for i := range 3 {
		if !l.allow("1.2.3.4") {
			t.Fatalf("attempt %d should be allowed", i)
		}
	}
	if l.allow("1.2.3.4") {
		t.Error("fourth attempt within the window should be denied")
	}

	// A different key has its own budget.
	if !l.allow("5.6.7.8") {
		t.Error("a different key should have its own budget")
	}

	// After the window slides past, the key is allowed again.
	now = now.Add(2 * time.Minute)
	if !l.allow("1.2.3.4") {
		t.Error("attempt after the window elapsed should be allowed")
	}
}

func TestLimiterRejectionDoesNotExtendLockout(t *testing.T) {
	now := time.Now()
	l := newLimiter(time.Minute, 1)
	l.now = func() time.Time { return now }

	if !l.allow("k") {
		t.Fatal("first attempt should be allowed")
	}
	// Hammer while maxed out; rejected attempts are not recorded.
	for range 5 {
		if l.allow("k") {
			t.Fatal("should stay denied while maxed out")
		}
	}
	// Once the single recorded hit ages out, the key is allowed again, proving
	// the rejected attempts did not push the lockout further into the future.
	now = now.Add(time.Minute + time.Second)
	if !l.allow("k") {
		t.Error("attempt after the one recorded hit expired should be allowed")
	}
}
