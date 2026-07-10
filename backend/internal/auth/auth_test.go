package auth

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

func newTestService(t *testing.T) (*Service, *db.DB) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return NewService(database, []byte("test-secret-please-ignore-0123456789")), database
}

func TestAuthenticateAndVerify(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()

	hash, err := HashPassword("hunter2hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateUser(ctx, "alice", hash, true); err != nil {
		t.Fatal(err)
	}

	user, pair, err := svc.Authenticate(ctx, "alice", "hunter2hunter2")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if !user.IsAdmin {
		t.Error("expected admin")
	}

	claims, err := svc.VerifyAccess(pair.AccessToken)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.UserID != user.ID || !claims.IsAdmin {
		t.Errorf("unexpected claims: %+v", claims)
	}
}

func TestAuthenticateWrongPassword(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	hash, _ := HashPassword("correct-horse")
	_, _ = database.CreateUser(ctx, "bob", hash, false)

	if _, _, err := svc.Authenticate(ctx, "bob", "wrong"); err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
	if _, _, err := svc.Authenticate(ctx, "ghost", "whatever"); err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials for unknown user, got %v", err)
	}
}

func TestRefreshRotation(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	hash, _ := HashPassword("password12345")
	_, _ = database.CreateUser(ctx, "carol", hash, false)

	_, pair, err := svc.Authenticate(ctx, "carol", "password12345")
	if err != nil {
		t.Fatal(err)
	}

	newPair, err := svc.Refresh(ctx, pair.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if newPair.RefreshToken == pair.RefreshToken {
		t.Error("refresh token should rotate")
	}
	// Old refresh token must now be rejected.
	if _, err := svc.Refresh(ctx, pair.RefreshToken); err != ErrInvalidToken {
		t.Errorf("expected old token rejected, got %v", err)
	}
}
