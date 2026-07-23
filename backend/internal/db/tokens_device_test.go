package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func deviceTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestDeviceSessionsGroupAcrossRotations(t *testing.T) {
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
	// One device, rotated once (old token revoked, new inserted, same id).
	if err := d.InsertRefreshToken(ctx, pid, "hash-a1", exp, "dev-a", "Phone"); err != nil {
		t.Fatal(err)
	}
	if err := d.RevokeRefreshToken(ctx, "hash-a1"); err != nil {
		t.Fatal(err)
	}
	if err := d.InsertRefreshToken(ctx, pid, "hash-a2", exp, "dev-a", "Phone"); err != nil {
		t.Fatal(err)
	}
	// A second device.
	if err := d.InsertRefreshToken(ctx, pid, "hash-b1", exp, "dev-b", "TV"); err != nil {
		t.Fatal(err)
	}
	// A pre-migration token with no device id stands alone.
	if err := d.InsertRefreshToken(ctx, pid, "hash-old", exp, "", ""); err != nil {
		t.Fatal(err)
	}

	sessions, err := d.ListDeviceSessions(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("expected 3 devices (rotation collapsed), got %d: %+v", len(sessions), sessions)
	}
	byKey := map[string]DeviceSession{}
	for _, s := range sessions {
		byKey[s.Key] = s
	}
	if byKey["dev-a"].DeviceName != "Phone" || byKey["dev-b"].DeviceName != "TV" {
		t.Errorf("unexpected sessions: %+v", sessions)
	}

	// Revoking a device kills all of its tokens; the legacy row revokes by key.
	if err := d.RevokeDeviceSession(ctx, "dev-a"); err != nil {
		t.Fatal(err)
	}
	sessions, _ = d.ListDeviceSessions(ctx)
	if len(sessions) != 2 {
		t.Fatalf("expected 2 after revoking dev-a, got %d", len(sessions))
	}
	if err := d.RevokeDeviceSession(ctx, "dev-a"); err != ErrNotFound {
		t.Errorf("revoking again should be ErrNotFound, got %v", err)
	}

	// Rotate-all clears the board.
	if err := d.RevokeAllTokens(ctx); err != nil {
		t.Fatal(err)
	}
	sessions, _ = d.ListDeviceSessions(ctx)
	if len(sessions) != 0 {
		t.Fatalf("expected none after revoke-all, got %d", len(sessions))
	}
}
