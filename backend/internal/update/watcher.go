package update

import (
	"context"
	"log/slog"
	"os"
	"time"
)

// Watcher periodically checks for a newer release and, once no stream is
// active, applies it and calls Restart so the caller can hand control back to
// the service manager (systemd/launchd/SCM), which brings the process back up
// running the new binary.
//
// Latest/HasUpdate/Apply/Restart are function values rather than a bound
// *Updater so tests can substitute fakes without touching the real GitHub
// API, disk, or process lifecycle.
type Watcher struct {
	CheckInterval time.Duration
	QuietPoll     time.Duration

	Latest         func(ctx context.Context) (*Release, error)
	HasUpdate      func(*Release) bool
	Apply          func(ctx context.Context, r *Release) error
	ActiveSessions func() int
	Restart        func()
}

// NewWatcher builds a Watcher backed by u, checking every 6 hours and polling
// every 2 minutes for a quiet window once an update is found. Restart defaults
// to os.Exit(0); the OS service manager (already configured to restart on
// exit, see internal/service) brings the new binary up.
func NewWatcher(u *Updater, activeSessions func() int) *Watcher {
	return &Watcher{
		CheckInterval:  6 * time.Hour,
		QuietPoll:      2 * time.Minute,
		Latest:         u.Latest,
		HasUpdate:      u.HasUpdate,
		Apply:          u.Apply,
		ActiveSessions: activeSessions,
		Restart:        func() { os.Exit(0) },
	}
}

// Run checks immediately, then on every CheckInterval, until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	w.checkOnce(ctx)
	ticker := time.NewTicker(w.CheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.checkOnce(ctx)
		}
	}
}

func (w *Watcher) checkOnce(ctx context.Context) {
	latest, err := w.Latest(ctx)
	if err != nil {
		slog.Warn("auto-update: check failed", "err", err)
		return
	}
	if !w.HasUpdate(latest) {
		return
	}
	slog.Info("auto-update: new release available, waiting for a quiet window", "version", latest.Version)
	if !w.waitForQuiet(ctx) {
		return
	}
	slog.Info("auto-update: applying", "version", latest.Version)
	if err := w.Apply(ctx, latest); err != nil {
		slog.Error("auto-update: apply failed", "err", err)
		return
	}
	slog.Info("auto-update: applied, restarting", "version", latest.Version)
	w.Restart()
}

// waitForQuiet blocks until ActiveSessions reports zero, polling at
// QuietPoll. Returns false if ctx is cancelled first.
func (w *Watcher) waitForQuiet(ctx context.Context) bool {
	for w.ActiveSessions() > 0 {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(w.QuietPoll):
		}
	}
	return true
}
