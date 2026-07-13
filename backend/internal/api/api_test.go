package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rhymeswithlimo/northrou/backend/internal/auth"
	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

// recordMailer captures the most recent pin so the test can complete flows.
type recordMailer struct {
	mu    sync.Mutex
	email string
	pin   string
}

func (m *recordMailer) SendPin(_ context.Context, email, pin string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.email, m.pin = email, pin
	return nil
}

func (m *recordMailer) last() (string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.email, m.pin
}

// testAPI wires a real DB + auth service and mounts the full router.
func testAPI(t *testing.T) (http.Handler, *recordMailer) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "api.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	mailer := &recordMailer{}
	authSvc := auth.NewService(database, []byte("test-secret-please-ignore-0123456789"), mailer)
	a := New(Deps{
		DB:         database,
		Auth:       authSvc,
		Cfg:        &config.Config{},
		ConfigPath: filepath.Join(dir, "config.toml"),
	})
	r := chi.NewRouter()
	a.Mount(r)
	return r, mailer
}

// do issues a request with an optional bearer token and decodes a JSON body.
func do(t *testing.T, h http.Handler, method, path, bearer string, body any, out any) int {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if out != nil && rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), out)
	}
	return rec.Code
}

// TestAuthProfilesEndToEnd drives the whole account/profile/elevation contract
// through the mounted router: setup, login + profile picker, profile switch,
// admin-OTP elevation, and the read/write admin gate.
func TestAuthProfilesEndToEnd(t *testing.T) {
	h, mailer := testAPI(t)
	const email = "owner@example.com"

	// needs_setup is true before setup.
	var status struct {
		NeedsSetup bool `json:"needs_setup"`
	}
	if do(t, h, http.MethodGet, "/api/setup/status", "", nil, &status); !status.NeedsSetup {
		t.Fatal("expected needs_setup=true before setup")
	}

	// Setup: creates account + first profile, returns an elevated session.
	var setupResp struct {
		Account struct {
			Email string `json:"email"`
		} `json:"account"`
		Profile struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"profile"`
		ConnectionCode string `json:"connection_code"`
		AccessToken    string `json:"access_token"`
		RefreshToken   string `json:"refresh_token"`
	}
	code := do(t, h, http.MethodPost, "/api/setup/complete", "",
		map[string]any{"email": email, "profile_name": "Owner"}, &setupResp)
	if code != http.StatusCreated {
		t.Fatalf("setup: status %d", code)
	}
	if setupResp.Account.Email != email || setupResp.Profile.Name != "Owner" || setupResp.ConnectionCode == "" {
		t.Fatalf("unexpected setup response: %+v", setupResp)
	}
	ownerID := setupResp.Profile.ID

	// The setup session is elevated: an admin mutation gets past RequireAdmin
	// (503 because no scanner is wired, not 403).
	if c := do(t, h, http.MethodPost, "/api/admin/scan", setupResp.AccessToken, nil, nil); c != http.StatusServiceUnavailable {
		t.Fatalf("elevated setup token should pass the admin gate, got %d", c)
	}

	// /me reflects the account, current profile, and elevated state.
	var me struct {
		Account  struct{ Email string } `json:"account"`
		Profile  struct{ Name string }  `json:"profile"`
		Profiles []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"profiles"`
		Admin bool `json:"admin"`
	}
	if c := do(t, h, http.MethodGet, "/api/me", setupResp.AccessToken, nil, &me); c != http.StatusOK {
		t.Fatalf("me: status %d", c)
	}
	if me.Account.Email != email || me.Profile.Name != "Owner" || !me.Admin {
		t.Fatalf("unexpected /me: %+v", me)
	}

	// Add a second profile (any signed-in profile may).
	var kids struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if c := do(t, h, http.MethodPost, "/api/profiles", setupResp.AccessToken,
		map[string]any{"name": "Kids"}, &kids); c != http.StatusCreated {
		t.Fatalf("create profile: status %d", c)
	}

	// Fresh device login: request + verify a pin.
	if c := do(t, h, http.MethodPost, "/api/auth/request-pin", "", map[string]any{"email": email}, nil); c != http.StatusOK {
		t.Fatalf("request-pin: status %d", c)
	}
	gotEmail, loginPin := mailer.last()
	if gotEmail != email || loginPin == "" {
		t.Fatalf("login pin not sent to account email: %q %q", gotEmail, loginPin)
	}
	var login struct {
		Profile struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"profile"`
		Profiles     []json.RawMessage `json:"profiles"`
		AccessToken  string            `json:"access_token"`
		RefreshToken string            `json:"refresh_token"`
	}
	if c := do(t, h, http.MethodPost, "/api/auth/verify-pin", "",
		map[string]any{"email": email, "pin": loginPin}, &login); c != http.StatusOK {
		t.Fatalf("verify-pin: status %d", c)
	}
	if len(login.Profiles) != 2 {
		t.Fatalf("verify-pin should return the picker list, got %d profiles", len(login.Profiles))
	}
	if login.Profile.ID != ownerID {
		t.Fatalf("verify-pin default should be the first profile, got %+v", login.Profile)
	}

	// A plain login token is NOT elevated: admin mutation is forbidden.
	if c := do(t, h, http.MethodPost, "/api/admin/scan", login.AccessToken, nil, nil); c != http.StatusForbidden {
		t.Fatalf("plain login token must be blocked from admin mutations, got %d", c)
	}
	// ...but admin reads are open to it: the request reaches the handler (503,
	// no scanner wired) rather than being turned away at the gate (403).
	if c := do(t, h, http.MethodGet, "/api/admin/scan", login.AccessToken, nil, nil); c == http.StatusForbidden || c == http.StatusUnauthorized {
		t.Fatalf("admin read should be open to any profile, got %d", c)
	}

	// Switch to the Kids profile with the device refresh token (no pin).
	var switched struct {
		Profile struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"profile"`
		AccessToken string `json:"access_token"`
	}
	if c := do(t, h, http.MethodPost, "/api/auth/select-profile", "",
		map[string]any{"refresh_token": login.RefreshToken, "profile_id": kids.ID}, &switched); c != http.StatusOK {
		t.Fatalf("select-profile: status %d", c)
	}
	if switched.Profile.Name != "Kids" {
		t.Fatalf("expected switch to Kids, got %+v", switched.Profile)
	}

	// Admin elevation via OTP, using the plain login token as the caller.
	if c := do(t, h, http.MethodPost, "/api/admin/request-otp", login.AccessToken, nil, nil); c != http.StatusOK {
		t.Fatalf("request-otp: status %d", c)
	}
	_, adminPin := mailer.last()
	if adminPin == "" || adminPin == loginPin {
		t.Fatalf("admin OTP not issued distinctly: %q", adminPin)
	}
	// Wrong code is rejected.
	if c := do(t, h, http.MethodPost, "/api/admin/verify-otp", login.AccessToken,
		map[string]any{"otp": "000000"}, nil); c != http.StatusUnauthorized {
		t.Fatalf("wrong admin otp should be 401, got %d", c)
	}
	var elevated struct {
		AccessToken string `json:"access_token"`
	}
	if c := do(t, h, http.MethodPost, "/api/admin/verify-otp", login.AccessToken,
		map[string]any{"otp": adminPin}, &elevated); c != http.StatusOK {
		t.Fatalf("verify-otp: status %d", c)
	}
	// The elevated token now passes the admin mutation gate.
	if c := do(t, h, http.MethodPost, "/api/admin/scan", elevated.AccessToken, nil, nil); c != http.StatusServiceUnavailable {
		t.Fatalf("elevated token should pass the admin gate, got %d", c)
	}
}

// TestDeleteLastProfileBlocked guards the invariant that the account can never
// be left with zero profiles.
func TestDeleteLastProfileBlocked(t *testing.T) {
	h, _ := testAPI(t)
	var setupResp struct {
		Profile     struct{ ID int64 `json:"id"` } `json:"profile"`
		AccessToken string                          `json:"access_token"`
	}
	if c := do(t, h, http.MethodPost, "/api/setup/complete", "",
		map[string]any{"email": "solo@example.com"}, &setupResp); c != http.StatusCreated {
		t.Fatalf("setup: status %d", c)
	}
	path := "/api/profiles/" + itoa(setupResp.Profile.ID)
	if c := do(t, h, http.MethodDelete, path, setupResp.AccessToken, nil, nil); c != http.StatusConflict {
		t.Fatalf("deleting the last profile must be 409, got %d", c)
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
