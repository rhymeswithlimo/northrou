package api

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rhymeswithlimo/northrou/backend/internal/auth"
	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

// sessionsAPI builds an API over a real database with one profile and two
// paired devices.
func sessionsAPI(t *testing.T) (*API, chi.Router, *db.DB, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	database, err := db.Open(filepath.Join(dir, "api.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := database.CreateAccount(context.Background()); err != nil {
		t.Fatal(err)
	}
	pid, err := database.CreateProfile(context.Background(), "Ada", "")
	if err != nil {
		t.Fatal(err)
	}

	authSvc := auth.NewService(database, []byte("test-secret-please-ignore-0123456789"))
	for _, name := range []string{"Kim's iPhone", "Living-room TV"} {
		if _, _, _, err := authSvc.IssueSession(context.Background(), auth.Device{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	_ = pid

	cfg := config.Default()
	cfg.Remote.Enabled = true
	cfg.Remote.ConnectionCode = "NR-OLDCO-DEOLD"
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}

	a := New(Deps{DB: database, Auth: authSvc, Cfg: cfg, ConfigPath: cfgPath})
	r := chi.NewRouter()
	a.Mount(r)
	return a, r, database, cfgPath
}

func TestListAndRevokeSessions(t *testing.T) {
	a, r, _, _ := sessionsAPI(t)
	tok := authedToken(t, a) // ephemeral: must NOT appear as a device

	var sessions []deviceSessionDTO
	if code := do(t, r, http.MethodGet, "/api/admin/sessions", tok, nil, &sessions); code != http.StatusOK {
		t.Fatalf("list sessions: got %d", code)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 devices, got %d: %+v", len(sessions), sessions)
	}
	byName := map[string]deviceSessionDTO{}
	for _, s := range sessions {
		byName[s.DeviceName] = s
		if s.ID == "" {
			t.Errorf("device %q has no id", s.DeviceName)
		}
	}
	phone, ok := byName["Kim's iPhone"]
	if !ok || byName["Living-room TV"].ID == "" {
		t.Fatalf("unexpected device names: %+v", sessions)
	}

	// An ephemeral local pair (the operator's own tooling) adds no device.
	if code := do(t, r, http.MethodPost, "/api/auth/pair", "", map[string]any{
		"ephemeral": true, "device_name": "Northrou CLI",
	}, nil); code != http.StatusOK {
		t.Fatalf("ephemeral pair: got %d", code)
	}
	if code := do(t, r, http.MethodGet, "/api/admin/sessions", tok, nil, &sessions); code != http.StatusOK || len(sessions) != 2 {
		t.Fatalf("ephemeral pair leaked into the device list: %d devices", len(sessions))
	}

	// Revoke one; the list shrinks.
	if code := do(t, r, http.MethodDelete, "/api/admin/sessions/"+phone.ID, tok, nil, nil); code != http.StatusNoContent {
		t.Fatalf("revoke: got %d", code)
	}
	if code := do(t, r, http.MethodGet, "/api/admin/sessions", tok, nil, &sessions); code != http.StatusOK {
		t.Fatal("re-list failed")
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 device after revoke, got %d", len(sessions))
	}

	// Revoking something unknown is a 404, not a silent success.
	if code := do(t, r, http.MethodDelete, "/api/admin/sessions/not-a-device", tok, nil, nil); code != http.StatusNotFound {
		t.Errorf("revoke unknown: got %d, want 404", code)
	}
}

func TestRotateConnectionCode(t *testing.T) {
	a, r, database, cfgPath := sessionsAPI(t)

	var res rotateCodeResponse
	if code := do(t, r, http.MethodPost, "/api/admin/connection-code/rotate", authedToken(t, a), map[string]any{}, &res); code != http.StatusOK {
		t.Fatalf("rotate: got %d", code)
	}
	if res.ConnectionCode == "" || res.ConnectionCode == "NR-OLDCO-DEOLD" {
		t.Fatalf("expected a fresh code, got %q", res.ConnectionCode)
	}

	// Persisted to disk and to the live config.
	onDisk, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if onDisk.Remote.ConnectionCode != res.ConnectionCode {
		t.Errorf("disk code %q != response %q", onDisk.Remote.ConnectionCode, res.ConnectionCode)
	}
	if a.Cfg.Remote.ConnectionCode != res.ConnectionCode {
		t.Errorf("live code %q != response %q", a.Cfg.Remote.ConnectionCode, res.ConnectionCode)
	}

	// Every session is gone: rotation without revocation is not a rotation.
	sessions, err := database.ListDeviceSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected all sessions revoked, still have %d", len(sessions))
	}
}

// authedToken mints a valid access token for the admin-read/mutation routes.
// The middleware only checks the JWT; admin-ness comes from the request being
// local, which httptest requests are. Ephemeral, so it never shows up as a
// paired device.
func authedToken(t *testing.T, a *API) string {
	t.Helper()
	ctx := context.Background()
	profiles, err := a.DB.ListProfiles(ctx)
	if err != nil || len(profiles) == 0 {
		t.Fatalf("no profiles: %v", err)
	}
	pair, err := a.Auth.IssueSetupSession(ctx, profiles[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return pair.AccessToken
}
