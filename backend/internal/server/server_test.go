package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/api"
	"github.com/rhymeswithlimo/northrou/backend/internal/auth"
	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "srv.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	a := api.New(api.Deps{
		DB:         database,
		Auth:       auth.NewService(database, []byte("test-secret-please-ignore-0123456789")),
		Cfg:        &config.Config{},
		ConfigPath: filepath.Join(dir, "config.toml"),
	})
	return New(":0", a).Handler()
}

// TestNoIPSpoofingViaHeaders is the regression guard for the critical admin-gate
// bypass: the server chain must NOT trust any client-supplied X-Real-IP /
// X-Forwarded-For (i.e. must not use chi's RealIP), because remote.IsLocal reads
// r.RemoteAddr to decide admin + code-free pairing. A direct request from a
// PUBLIC peer that spoofs a loopback header must stay untrusted → setup (which
// is local-only) is forbidden.
func TestNoIPSpoofingViaHeaders(t *testing.T) {
	h := testHandler(t)
	for _, hdr := range []string{"X-Real-IP", "X-Forwarded-For", "True-Client-IP"} {
		req := httptest.NewRequest(http.MethodPost, "/api/setup/complete", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.5:40000" // public peer
		req.Header.Set(hdr, "127.0.0.1")     // spoof loopback
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s spoof: setup from a public peer must be 403 (untrusted), got %d", hdr, rec.Code)
		}
	}
}

func TestAllowedOrigin(t *testing.T) {
	allowed := []string{"", "tauri://localhost", "https://tauri.localhost", "http://localhost:5173", "http://127.0.0.1:8674", "https://localhost"}
	denied := []string{"https://evil.example", "http://attacker.com", "http://localhost.evil.com", "null"}
	for _, o := range allowed {
		if !allowedOrigin(o) {
			t.Errorf("origin %q should be allowed", o)
		}
	}
	for _, o := range denied {
		if allowedOrigin(o) {
			t.Errorf("origin %q should be denied", o)
		}
	}
}

// TestCORSDoesNotReflectArbitraryOrigin proves a hostile Origin gets no
// Access-Control-Allow-Origin, so a drive-by page cannot read API responses.
func TestCORSDoesNotReflectArbitraryOrigin(t *testing.T) {
	h := testHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("hostile origin must not be reflected, got ACAO=%q", got)
	}
}
