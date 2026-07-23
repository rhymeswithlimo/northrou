package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rhymeswithlimo/northrou/backend/internal/auth"
	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/rhymeswithlimo/northrou/backend/internal/db"
	"github.com/rhymeswithlimo/northrou/backend/internal/remote"
)

// testAPI wires a real DB + auth service and mounts the full router.
func testAPI(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "api.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	authSvc := auth.NewService(database, []byte("test-secret-please-ignore-0123456789"))
	a := New(Deps{
		DB:         database,
		Auth:       authSvc,
		Cfg:        &config.Config{},
		ConfigPath: filepath.Join(dir, "config.toml"),
	})
	r := chi.NewRouter()
	a.Mount(r)
	return r
}

// do issues a LOCAL request (a loopback peer, as a same-origin browser / CLI on
// the box would) with an optional bearer token and decodes a JSON body.
func do(t *testing.T, h http.Handler, method, path, bearer string, body any, out any) int {
	return doReq(t, h, method, path, bearer, body, out, "local")
}

// doTunnel issues a request marked as arriving over the WebRTC tunnel (a remote
// client), which is how the box tells local from remote for the admin gate.
func doTunnel(t *testing.T, h http.Handler, method, path, bearer string, body any, out any) int {
	return doReq(t, h, method, path, bearer, body, out, "tunnel")
}

// doPublic issues a direct (non-tunnel) request from a PUBLIC peer IP, as would
// happen if the box's HTTP port were exposed to the internet. Such a request
// must be treated like a remote client: no code-free pairing, no admin.
func doPublic(t *testing.T, h http.Handler, method, path, bearer string, body any, out any) int {
	return doReq(t, h, method, path, bearer, body, out, "public")
}

// mode is "local" (loopback peer), "tunnel" (marked tunneled), or "public"
// (a public source IP on the direct path).
func doReq(t *testing.T, h http.Handler, method, path, bearer string, body, out any, mode string) int {
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
	switch mode {
	case "tunnel":
		req = req.WithContext(remote.WithTunnel(req.Context()))
	case "local":
		req.RemoteAddr = "127.0.0.1:40000" // loopback: trusted
	case "public":
		req.RemoteAddr = "203.0.113.5:40000" // TEST-NET-3: a public IP
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if out != nil && rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), out)
	}
	return rec.Code
}

// TestAuthProfilesEndToEnd drives the whole setup/pair/profile/admin contract
// through the mounted router: local setup, the local-vs-tunnel admin gate,
// remote pairing with the connection code, the profile picker + switch.
func TestAuthProfilesEndToEnd(t *testing.T) {
	h := testAPI(t)

	// needs_setup is true before setup.
	var status struct {
		NeedsSetup bool `json:"needs_setup"`
	}
	if do(t, h, http.MethodGet, "/api/setup/status", "", nil, &status); !status.NeedsSetup {
		t.Fatal("expected needs_setup=true before setup")
	}

	// Setup (local): creates account + first profile, returns the connection code
	// and a signed-in session.
	var setupResp struct {
		Profile struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"profile"`
		ConnectionCode string `json:"connection_code"`
		AccessToken    string `json:"access_token"`
		RefreshToken   string `json:"refresh_token"`
	}
	code := do(t, h, http.MethodPost, "/api/setup/complete", "",
		map[string]any{"profile_name": "Owner"}, &setupResp)
	if code != http.StatusCreated {
		t.Fatalf("setup: status %d", code)
	}
	if setupResp.Profile.Name != "Owner" || setupResp.ConnectionCode == "" {
		t.Fatalf("unexpected setup response: %+v", setupResp)
	}
	ownerID := setupResp.Profile.ID
	connCode := setupResp.ConnectionCode

	// Setup cannot be run over the tunnel.
	if c := doTunnel(t, h, http.MethodPost, "/api/setup/complete", "", map[string]any{}, nil); c != http.StatusForbidden {
		t.Fatalf("setup over the tunnel must be forbidden, got %d", c)
	}

	// A local request may perform an admin mutation: it gets past RequireLocal
	// (503 because no scanner is wired, not 403).
	if c := do(t, h, http.MethodPost, "/api/admin/scan", setupResp.AccessToken, nil, nil); c != http.StatusServiceUnavailable {
		t.Fatalf("local admin mutation should pass the gate, got %d", c)
	}

	// /me over a local request reports admin:true.
	var me struct {
		Profile struct{ Name string } `json:"profile"`
		Admin   bool                  `json:"admin"`
	}
	if c := do(t, h, http.MethodGet, "/api/me", setupResp.AccessToken, nil, &me); c != http.StatusOK {
		t.Fatalf("me: status %d", c)
	}
	if me.Profile.Name != "Owner" || !me.Admin {
		t.Fatalf("unexpected local /me: %+v", me)
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

	// Remote pairing: a tunnel pair with a wrong code is rejected...
	if c := doTunnel(t, h, http.MethodPost, "/api/auth/pair", "", map[string]any{"code": "NR-WRONG-CODE0"}, nil); c != http.StatusUnauthorized {
		t.Fatalf("wrong connection code should be 401, got %d", c)
	}
	// ...and the correct code (case/format-insensitive) yields a session + picker.
	var paired struct {
		Profile struct {
			ID int64 `json:"id"`
		} `json:"profile"`
		Profiles     []json.RawMessage `json:"profiles"`
		AccessToken  string            `json:"access_token"`
		RefreshToken string            `json:"refresh_token"`
	}
	if c := doTunnel(t, h, http.MethodPost, "/api/auth/pair", "", map[string]any{"code": connCode}, &paired); c != http.StatusOK {
		t.Fatalf("pair with correct code: status %d", c)
	}
	if len(paired.Profiles) != 2 {
		t.Fatalf("pair should return the picker list, got %d profiles", len(paired.Profiles))
	}
	if paired.Profile.ID != ownerID {
		t.Fatalf("pair default should be the first profile, got %+v", paired.Profile)
	}

	// The paired (remote) session is NOT admin: /me over the tunnel reports false,
	// admin mutations are forbidden, but admin reads stay open.
	if c := doTunnel(t, h, http.MethodGet, "/api/me", paired.AccessToken, nil, &me); c != http.StatusOK || me.Admin {
		t.Fatalf("remote /me should be admin:false, got status=%d admin=%v", c, me.Admin)
	}
	if c := doTunnel(t, h, http.MethodPost, "/api/admin/scan", paired.AccessToken, nil, nil); c != http.StatusForbidden {
		t.Fatalf("remote admin mutation must be blocked, got %d", c)
	}
	if c := doTunnel(t, h, http.MethodGet, "/api/admin/scan", paired.AccessToken, nil, nil); c == http.StatusForbidden || c == http.StatusUnauthorized {
		t.Fatalf("admin read should be open to a remote session, got %d", c)
	}

	// A local pair needs no code.
	if c := do(t, h, http.MethodPost, "/api/auth/pair", "", map[string]any{}, nil); c != http.StatusOK {
		t.Fatalf("local pair without a code should succeed, got %d", c)
	}

	// Switch to the Kids profile with the device refresh token.
	var switched struct {
		Profile struct {
			Name string `json:"name"`
		} `json:"profile"`
	}
	if c := doTunnel(t, h, http.MethodPost, "/api/auth/select-profile", "",
		map[string]any{"refresh_token": paired.RefreshToken, "profile_id": kids.ID}, &switched); c != http.StatusOK {
		t.Fatalf("select-profile: status %d", c)
	}
	if switched.Profile.Name != "Kids" {
		t.Fatalf("expected switch to Kids, got %+v", switched.Profile)
	}

	// Deleting a profile is an admin mutation: refused over the tunnel, allowed
	// locally.
	kidsPath := "/api/profiles/" + itoa(kids.ID)
	if c := doTunnel(t, h, http.MethodDelete, kidsPath, paired.AccessToken, nil, nil); c != http.StatusForbidden {
		t.Fatalf("remote delete must be blocked, got %d", c)
	}
	if c := do(t, h, http.MethodDelete, kidsPath, setupResp.AccessToken, nil, nil); c != http.StatusNoContent {
		t.Fatalf("local delete should succeed, got %d", c)
	}
}

// TestPublicDirectRequestIsNotTrusted guards the security boundary that makes
// "local = admin" safe when the box's HTTP port is exposed: a direct (non-tunnel)
// request from a PUBLIC source IP must be treated exactly like a remote client —
// no code-free pairing, no admin — even though it did not come over the tunnel.
func TestPublicDirectRequestIsNotTrusted(t *testing.T) {
	h := testAPI(t)

	// Set the box up locally and grab its connection code.
	var setupResp struct {
		ConnectionCode string `json:"connection_code"`
	}
	if c := do(t, h, http.MethodPost, "/api/setup/complete", "", map[string]any{"profile_name": "Owner"}, &setupResp); c != http.StatusCreated {
		t.Fatalf("setup: status %d", c)
	}

	// A public-IP request may NOT pair without the code (the hole would be a
	// code-free session here).
	if c := doPublic(t, h, http.MethodPost, "/api/auth/pair", "", map[string]any{}, nil); c != http.StatusUnauthorized {
		t.Fatalf("code-free pair from a public IP must be 401, got %d", c)
	}

	// With the correct code it pairs, but the session is NOT admin...
	var paired struct {
		AccessToken string `json:"access_token"`
	}
	if c := doPublic(t, h, http.MethodPost, "/api/auth/pair", "", map[string]any{"code": setupResp.ConnectionCode}, &paired); c != http.StatusOK {
		t.Fatalf("pair from a public IP with the code: status %d", c)
	}
	var me struct {
		Admin bool `json:"admin"`
	}
	if c := doPublic(t, h, http.MethodGet, "/api/me", paired.AccessToken, nil, &me); c != http.StatusOK || me.Admin {
		t.Fatalf("public-IP session must be admin:false, got status=%d admin=%v", c, me.Admin)
	}
	// ...and it cannot perform admin mutations.
	if c := doPublic(t, h, http.MethodPost, "/api/admin/scan", paired.AccessToken, nil, nil); c != http.StatusForbidden {
		t.Fatalf("admin mutation from a public IP must be 403, got %d", c)
	}
	// Setup over a public IP is refused too.
	if c := doPublic(t, h, http.MethodPost, "/api/setup/complete", "", map[string]any{}, nil); c != http.StatusForbidden {
		t.Fatalf("setup from a public IP must be 403, got %d", c)
	}
}

// TestDeleteLastProfileBlocked guards the invariant that the account can never
// be left with zero profiles.
func TestDeleteLastProfileBlocked(t *testing.T) {
	h := testAPI(t)
	var setupResp struct {
		Profile struct {
			ID int64 `json:"id"`
		} `json:"profile"`
		AccessToken string `json:"access_token"`
	}
	if c := do(t, h, http.MethodPost, "/api/setup/complete", "",
		map[string]any{"profile_name": "Solo"}, &setupResp); c != http.StatusCreated {
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
