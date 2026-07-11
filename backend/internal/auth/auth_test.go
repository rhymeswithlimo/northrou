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

func TestRequestAndVerifyPin(t *testing.T) {
	svc, database, mailer := newTestService(t)
	ctx := context.Background()

	if _, err := database.CreateUser(ctx, "alice@example.com", true); err != nil {
		t.Fatal(err)
	}

	// Email is normalized on lookup, so a differently-cased request still works.
	if err := svc.RequestPin(ctx, "Alice@Example.com"); err != nil {
		t.Fatalf("request pin: %v", err)
	}
	if mailer.calls != 1 || mailer.pin == "" {
		t.Fatalf("expected a pin to be sent, got %+v", mailer)
	}

	user, pair, err := svc.VerifyPin(ctx, "alice@example.com", mailer.pin)
	if err != nil {
		t.Fatalf("verify pin: %v", err)
	}
	if !user.IsAdmin {
		t.Error("expected admin")
	}

	claims, err := svc.VerifyAccess(pair.AccessToken)
	if err != nil {
		t.Fatalf("verify access: %v", err)
	}
	if claims.UserID != user.ID || !claims.IsAdmin {
		t.Errorf("unexpected claims: %+v", claims)
	}

	// A consumed pin cannot be reused.
	if _, _, err := svc.VerifyPin(ctx, "alice@example.com", mailer.pin); err != ErrInvalidCredentials {
		t.Errorf("expected consumed pin rejected, got %v", err)
	}
}

func TestVerifyPinWrongAndUnknown(t *testing.T) {
	svc, database, mailer := newTestService(t)
	ctx := context.Background()
	if _, err := database.CreateUser(ctx, "bob@example.com", false); err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestPin(ctx, "bob@example.com"); err != nil {
		t.Fatal(err)
	}

	if _, _, err := svc.VerifyPin(ctx, "bob@example.com", "000000"); err != ErrInvalidCredentials {
		t.Errorf("expected wrong pin rejected, got %v", err)
	}
	// The correct pin is unaffected by an earlier wrong guess.
	if _, _, err := svc.VerifyPin(ctx, "bob@example.com", mailer.pin); err != nil {
		t.Errorf("correct pin should verify, got %v", err)
	}

	// Unknown email must not reveal itself: request is silent, verify fails.
	if err := svc.RequestPin(ctx, "ghost@example.com"); err != nil {
		t.Errorf("request for unknown email should be silent, got %v", err)
	}
	if _, _, err := svc.VerifyPin(ctx, "ghost@example.com", "123456"); err != ErrInvalidCredentials {
		t.Errorf("expected unknown email rejected, got %v", err)
	}
}

func TestPinAttemptCap(t *testing.T) {
	svc, database, mailer := newTestService(t)
	ctx := context.Background()
	if _, err := database.CreateUser(ctx, "dave@example.com", false); err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestPin(ctx, "dave@example.com"); err != nil {
		t.Fatal(err)
	}

	for i := range maxPinAttempts {
		if _, _, err := svc.VerifyPin(ctx, "dave@example.com", "999999"); err != ErrInvalidCredentials {
			t.Fatalf("attempt %d: expected rejection, got %v", i, err)
		}
	}
	// After the cap, even the correct pin is refused (it was invalidated).
	if _, _, err := svc.VerifyPin(ctx, "dave@example.com", mailer.pin); err != ErrInvalidCredentials {
		t.Errorf("expected pin locked after attempt cap, got %v", err)
	}
}

func TestRefreshRotation(t *testing.T) {
	svc, database, mailer := newTestService(t)
	ctx := context.Background()
	if _, err := database.CreateUser(ctx, "carol@example.com", false); err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestPin(ctx, "carol@example.com"); err != nil {
		t.Fatal(err)
	}
	_, pair, err := svc.VerifyPin(ctx, "carol@example.com", mailer.pin)
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
