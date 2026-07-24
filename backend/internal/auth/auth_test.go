package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

func newTestService(t *testing.T) (*Service, *db.DB) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	svc := NewService(database, []byte("test-secret-please-ignore-0123456789"))
	return svc, database
}

// setupAccount marks setup complete and creates one profile, returning its id.
// Mirrors what first-run setup does.
func setupAccount(t *testing.T, database *db.DB, profile string) int64 {
	t.Helper()
	ctx := context.Background()
	if err := database.CreateAccount(ctx); err != nil {
		t.Fatalf("create account: %v", err)
	}
	id, err := database.CreateProfile(ctx, profile, "")
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	return id
}

func TestIssueSessionDefaultsToFirstProfile(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	pid := setupAccount(t, database, "Alice")

	profiles, selected, pair, err := svc.IssueSession(ctx, Device{Name: "test"})
	if err != nil {
		t.Fatalf("issue session: %v", err)
	}
	if len(profiles) != 1 || selected.ID != pid {
		t.Fatalf("expected default profile %d, got %+v", pid, selected)
	}

	claims, err := svc.VerifyAccess(pair.AccessToken)
	if err != nil {
		t.Fatalf("verify access: %v", err)
	}
	if claims.ProfileID != pid {
		t.Errorf("claims profile = %d, want %d", claims.ProfileID, pid)
	}
}

func TestIssueSessionNoProfiles(t *testing.T) {
	svc, database := newTestService(t)
	if err := database.CreateAccount(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := svc.IssueSession(context.Background(), Device{Name: "test"}); err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials with no profiles, got %v", err)
	}
}

func TestRefreshRotationKeepsProfile(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	pid := setupAccount(t, database, "Carol")

	_, _, pair, err := svc.IssueSession(ctx, Device{Name: "test"})
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
	claims, err := svc.VerifyAccess(newPair.AccessToken)
	if err != nil || claims.ProfileID != pid {
		t.Errorf("refreshed token should keep profile %d, got %+v (%v)", pid, claims, err)
	}
	// Old refresh token must now be rejected.
	if _, err := svc.Refresh(ctx, pair.RefreshToken); err != ErrInvalidToken {
		t.Errorf("expected old token rejected, got %v", err)
	}
}

func TestLogoutRevokesRefresh(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	setupAccount(t, database, "Erin")
	_, _, pair, err := svc.IssueSession(ctx, Device{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout(ctx, pair.RefreshToken); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := svc.Refresh(ctx, pair.RefreshToken); err != ErrInvalidToken {
		t.Errorf("expected revoked token rejected, got %v", err)
	}
}

func TestSelectProfileSwitches(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()
	setupAccount(t, database, "Kira")
	kid, err := database.CreateProfile(ctx, "Kids", "")
	if err != nil {
		t.Fatal(err)
	}
	_, selected, pair, err := svc.IssueSession(ctx, Device{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Name != "Kira" {
		t.Fatalf("default should be first profile, got %s", selected.Name)
	}

	prof, newPair, err := svc.SelectProfile(ctx, pair.RefreshToken, kid)
	if err != nil {
		t.Fatalf("select profile: %v", err)
	}
	if prof.ID != kid {
		t.Errorf("switched to %d, want %d", prof.ID, kid)
	}
	claims, _ := svc.VerifyAccess(newPair.AccessToken)
	if claims.ProfileID != kid {
		t.Errorf("token profile = %d, want %d", claims.ProfileID, kid)
	}
	// The old refresh token was rotated away.
	if _, _, err := svc.SelectProfile(ctx, pair.RefreshToken, kid); err != ErrInvalidToken {
		t.Errorf("expected rotated refresh rejected, got %v", err)
	}
	// Switching to a nonexistent profile fails.
	if _, _, err := svc.SelectProfile(ctx, newPair.RefreshToken, 9999); err != ErrInvalidCredentials {
		t.Errorf("expected unknown profile rejected, got %v", err)
	}
}

func TestStreamTokenScopingAndFileBinding(t *testing.T) {
	svc, database := newTestService(t)
	pid := setupAccount(t, database, "Alice")

	tok, exp, err := svc.IssueStreamToken(pid, 42)
	if err != nil {
		t.Fatalf("issue stream token: %v", err)
	}
	if time.Until(exp) < 10*time.Hour {
		t.Fatalf("stream token should be long-lived; expires in %s", time.Until(exp))
	}

	// A stream token must NOT authenticate a full API session.
	if _, err := svc.VerifyAccess(tok); err == nil {
		t.Fatal("VerifyAccess accepted a stream token; it must reject scoped tokens")
	}

	// It IS valid on the media routes, and bound to its file.
	claims, err := svc.VerifyMedia(tok)
	if err != nil {
		t.Fatalf("VerifyMedia rejected a stream token: %v", err)
	}
	if claims.ProfileID != pid {
		t.Errorf("stream token profile = %d, want %d", claims.ProfileID, pid)
	}
	if !claims.AllowsFile(42) {
		t.Error("stream token should allow its own file 42")
	}
	if claims.AllowsFile(43) {
		t.Error("stream token must NOT allow a different file 43")
	}

	// A normal session token works on media routes too, and is file-agnostic.
	_, _, apair, err := svc.IssueSession(context.Background(), Device{Name: "t"})
	if err != nil {
		t.Fatalf("issue session: %v", err)
	}
	acc, err := svc.VerifyMedia(apair.AccessToken)
	if err != nil {
		t.Fatalf("VerifyMedia rejected a normal token: %v", err)
	}
	if !acc.AllowsFile(999) {
		t.Error("a full-access token should allow any file")
	}
}
