package scanner

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitWhilePlaying_PausesThenResumes(t *testing.T) {
	s := &Scanner{}
	var active atomic.Int32
	active.Store(1)
	s.SetPlaybackGate(func() int { return int(active.Load()) })

	done := make(chan struct{})
	go func() {
		s.waitWhilePlaying(context.Background())
		close(done)
	}()

	// While a stream is active the scan must stay parked.
	select {
	case <-done:
		t.Fatal("scan resumed while playback was active")
	case <-time.After(50 * time.Millisecond):
	}

	// Once playback stops it resumes on its own.
	active.Store(0)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scan did not resume after playback stopped")
	}
}

func TestWaitWhilePlaying_NoGateDoesNotBlock(t *testing.T) {
	s := &Scanner{}
	done := make(chan struct{})
	go func() {
		s.waitWhilePlaying(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("with no gate wired, the scan must not block")
	}
}

func TestWaitWhilePlaying_ContextCancelUnblocks(t *testing.T) {
	s := &Scanner{}
	s.SetPlaybackGate(func() int { return 1 }) // never idle
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.waitWhilePlaying(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("context cancellation should unblock a parked scan")
	}
}
