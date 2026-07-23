package db

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRotateRefreshTokenConcurrentDoubleSpend(t *testing.T) {
	d := deviceTestDB(t)
	ctx := context.Background()
	if err := d.CreateAccount(ctx); err != nil {
		t.Fatal(err)
	}
	pid, err := d.CreateProfile(ctx, "Ada", "")
	if err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(time.Hour)
	if err := d.InsertRefreshToken(ctx, pid, "old", exp, "dev1", "Phone"); err != nil {
		t.Fatal(err)
	}
	// Two goroutines race to rotate the SAME token. Exactly one may win.
	var wg sync.WaitGroup
	var okCount int
	var mu sync.Mutex
	for i, nh := range []string{"newA", "newB"} {
		wg.Add(1)
		go func(i int, newHash string) {
			defer wg.Done()
			if _, err := d.RotateRefreshToken(ctx, "old", newHash, exp, 0); err == nil {
				mu.Lock()
				okCount++
				mu.Unlock()
			}
		}(i, nh)
	}
	wg.Wait()
	if okCount != 1 {
		t.Fatalf("exactly one concurrent rotation should win, got %d", okCount)
	}
}

func TestRotateRefreshTokenReuseNukesFamily(t *testing.T) {
	d := deviceTestDB(t)
	ctx := context.Background()
	if err := d.CreateAccount(ctx); err != nil {
		t.Fatal(err)
	}
	pid, _ := d.CreateProfile(ctx, "Ada", "")
	exp := time.Now().Add(time.Hour)
	_ = d.InsertRefreshToken(ctx, pid, "t1", exp, "dev1", "Phone")

	// Legit rotation t1 -> t2.
	if _, err := d.RotateRefreshToken(ctx, "t1", "t2", exp, 0); err != nil {
		t.Fatalf("first rotation: %v", err)
	}
	// Replaying the already-rotated t1 is reuse: it must error and revoke the
	// whole device family, including the live t2.
	if _, err := d.RotateRefreshToken(ctx, "t1", "t3", exp, 0); !errors.Is(err, ErrTokenReused) {
		t.Fatalf("replay should be ErrTokenReused, got %v", err)
	}
	if _, err := d.RotateRefreshToken(ctx, "t2", "t4", exp, 0); err == nil {
		t.Fatal("t2 should have been revoked by reuse detection")
	}
}
