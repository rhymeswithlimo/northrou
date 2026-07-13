package auth

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

// captureMailer records the last pin "sent" so tests can complete the flow.
type captureMailer struct {
	mu    sync.Mutex
	email string
	pin   string
	calls int
}

func (m *captureMailer) SendPin(_ context.Context, email, pin string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.email, m.pin, m.calls = email, pin, m.calls+1
	return nil
}

func newTestService(t *testing.T) (*Service, *db.DB, *captureMailer) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	mailer := &captureMailer{}
	svc := NewService(database, []byte("test-secret-please-ignore-0123456789"), mailer)
	return svc, database, mailer
}

// setupAccount establishes the singleton account and one profile, returning the
// profile id. Mirrors what first-run setup does.
func setupAccount(t *testing.T, database *db.DB, email, profile string) int64 {
	t.Helper()
	ctx := context.Background()
	if err := database.SetAccountEmail(ctx, email); err != nil {
		t.Fatalf("set account: %v", err)
	}
	id, err := database.CreateProfile(ctx, profile, "")
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	return id
}

func TestRequestAndVerifyLoginPin(t *testing.T) {
	svc, database, mailer := newTestService(t)
	ctx := context.Background()
	pid := setupAccount(t, database, "alice@example.com", "Alice")

	// Email is normalized, so a differently-cased request still matches.
	if err := svc.RequestLoginPin(ctx, "Alice@Example.com"); err != nil {
		t.Fatalf("request pin: %v", err)
	}
	if mailer.calls != 1 || mailer.pin == "" {
		t.Fatalf("expected a pin to be sent, got %+v", mailer)
	}

	profiles, selected, pair, err := svc.VerifyLoginPin(ctx, "alice@example.com", mailer.pin)
	if err != nil {
		t.Fatalf("verify pin: %v", err)
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
	if claims.Admin {
		t.Error("a login token must not be admin-elevated")
	}

	// A consumed pin cannot be reused.
	if _, _, _, err := svc.VerifyLoginPin(ctx, "alice@example.com", mailer.pin); err != ErrInvalidCredentials {
		t.Errorf("expected consumed pin rejected, got %v", err)
	}
}

func TestVerifyLoginPinWrongAndWrongEmail(t *testing.T) {
	svc, database, mailer := newTestService(t)
	ctx := context.Background()
	setupAccount(t, database, "bob@example.com", "Bob")
	if err := svc.RequestLoginPin(ctx, "bob@example.com"); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := svc.VerifyLoginPin(ctx, "bob@example.com", "000000"); err != ErrInvalidCredentials {
		t.Errorf("expected wrong pin rejected, got %v", err)
	}
	// The correct pin is unaffected by an earlier wrong guess.
	if _, _, _, err := svc.VerifyLoginPin(ctx, "bob@example.com", mailer.pin); err != nil {
		t.Errorf("correct pin should verify, got %v", err)
	}

	// A non-account email must not reveal itself: request is silent...
	before := mailer.calls
	if err := svc.RequestLoginPin(ctx, "ghost@example.com"); err != nil {
		t.Errorf("request for non-account email should be silent, got %v", err)
	}
	if mailer.calls != before {
		t.Error("no pin should be sent for a non-account email")
	}
	// ...and verify fails.
	if _, _, _, err := svc.VerifyLoginPin(ctx, "ghost@example.com", "123456"); err != ErrInvalidCredentials {
		t.Errorf("expected non-account email rejected, got %v", err)
	}
}

func TestPinAttemptCap(t *testing.T) {
	svc, database, mailer := newTestService(t)
	ctx := context.Background()
	setupAccount(t, database, "dave@example.com", "Dave")
	if err := svc.RequestLoginPin(ctx, "dave@example.com"); err != nil {
		t.Fatal(err)
	}

	for i := range maxPinAttempts {
		if _, _, _, err := svc.VerifyLoginPin(ctx, "dave@example.com", "999999"); err != ErrInvalidCredentials {
			t.Fatalf("attempt %d: expected rejection, got %v", i, err)
		}
	}
	// After the cap, even the correct pin is refused (it was invalidated).
	if _, _, _, err := svc.VerifyLoginPin(ctx, "dave@example.com", mailer.pin); err != ErrInvalidCredentials {
		t.Errorf("expected pin locked after attempt cap, got %v", err)
	}
}

func TestRefreshRotationKeepsProfile(t *testing.T) {
	svc, database, mailer := newTestService(t)
	ctx := context.Background()
	pid := setupAccount(t, database, "carol@example.com", "Carol")
	if err := svc.RequestLoginPin(ctx, "carol@example.com"); err != nil {
		t.Fatal(err)
	}
	_, _, pair, err := svc.VerifyLoginPin(ctx, "carol@example.com", mailer.pin)
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

func TestSelectProfileSwitches(t *testing.T) {
	svc, database, mailer := newTestService(t)
	ctx := context.Background()
	setupAccount(t, database, "home@example.com", "Kira")
	kid, err := database.CreateProfile(ctx, "Kids", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestLoginPin(ctx, "home@example.com"); err != nil {
		t.Fatal(err)
	}
	_, selected, pair, err := svc.VerifyLoginPin(ctx, "home@example.com", mailer.pin)
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

func TestAdminOTPElevation(t *testing.T) {
	svc, database, mailer := newTestService(t)
	ctx := context.Background()
	pid := setupAccount(t, database, "owner@example.com", "Owner")

	if err := svc.RequestAdminOTP(ctx); err != nil {
		t.Fatalf("request admin otp: %v", err)
	}
	if mailer.pin == "" || mailer.email != "owner@example.com" {
		t.Fatalf("admin otp not sent to account email: %+v", mailer)
	}

	// A wrong code does not elevate.
	if _, _, err := svc.VerifyAdminOTP(ctx, pid, "000000"); err != ErrInvalidCredentials {
		t.Errorf("expected wrong otp rejected, got %v", err)
	}
	token, _, err := svc.VerifyAdminOTP(ctx, pid, mailer.pin)
	if err != nil {
		t.Fatalf("verify admin otp: %v", err)
	}
	claims, err := svc.VerifyAccess(token)
	if err != nil {
		t.Fatalf("verify elevated token: %v", err)
	}
	if !claims.Admin || claims.ProfileID != pid {
		t.Errorf("expected elevated token for profile %d, got %+v", pid, claims)
	}
}
