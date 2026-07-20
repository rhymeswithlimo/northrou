package update

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func testWatcher() *Watcher {
	return &Watcher{
		CheckInterval:  5 * time.Millisecond,
		QuietPoll:      5 * time.Millisecond,
		ActiveSessions: func() int { return 0 },
	}
}

// TestWatcherAppliesOnceQuiet checks that the watcher waits out active
// sessions before applying, and stops after Restart is called.
func TestWatcherAppliesOnceQuiet(t *testing.T) {
	var sessions atomic.Int32
	sessions.Store(2)

	var applied atomic.Bool
	var restarted atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := testWatcher()
	w.Latest = func(ctx context.Context) (*Release, error) {
		return &Release{Version: "v2.0.0"}, nil
	}
	w.HasUpdate = func(r *Release) bool { return r.Version == "v2.0.0" }
	w.ActiveSessions = func() int { return int(sessions.Load()) }
	w.Apply = func(ctx context.Context, r *Release) error {
		applied.Store(true)
		return nil
	}
	w.Restart = func() {
		restarted.Store(true)
		cancel()
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		sessions.Store(0)
	}()

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher never finished")
	}

	if !applied.Load() {
		t.Error("expected Apply to be called")
	}
	if !restarted.Load() {
		t.Error("expected Restart to be called")
	}
}

// TestWatcherNoUpdateNeverApplies checks that Apply/Restart are never called
// when HasUpdate reports false.
func TestWatcherNoUpdateNeverApplies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	w := testWatcher()
	w.Latest = func(ctx context.Context) (*Release, error) {
		return &Release{Version: "v1.0.0"}, nil
	}
	w.HasUpdate = func(r *Release) bool { return false }
	w.Apply = func(ctx context.Context, r *Release) error {
		t.Error("Apply should not be called")
		return nil
	}
	w.Restart = func() {
		t.Error("Restart should not be called")
	}

	w.Run(ctx)
}

// TestWatcherCheckErrorDoesNotCrash checks that a failed check is logged and
// retried rather than treated as fatal.
func TestWatcherCheckErrorDoesNotCrash(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	var checks atomic.Int32
	w := testWatcher()
	w.Latest = func(ctx context.Context) (*Release, error) {
		checks.Add(1)
		return nil, errors.New("network down")
	}
	w.HasUpdate = func(r *Release) bool {
		t.Error("HasUpdate should not be reached after a failed check")
		return false
	}

	w.Run(ctx)

	if checks.Load() < 2 {
		t.Errorf("expected the watcher to retry after a failed check, got %d attempts", checks.Load())
	}
}

// TestWatcherApplyErrorRetries checks that a failed Apply is logged and the
// watcher keeps running rather than restarting on a broken update.
func TestWatcherApplyErrorRetries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	var applies atomic.Int32
	w := testWatcher()
	w.Latest = func(ctx context.Context) (*Release, error) {
		return &Release{Version: "v2.0.0"}, nil
	}
	w.HasUpdate = func(r *Release) bool { return true }
	w.Apply = func(ctx context.Context, r *Release) error {
		applies.Add(1)
		return errors.New("checksum mismatch")
	}
	w.Restart = func() {
		t.Error("Restart should not be called when Apply fails")
	}

	w.Run(ctx)

	if applies.Load() < 2 {
		t.Errorf("expected the watcher to retry Apply on the next check, got %d attempts", applies.Load())
	}
}
