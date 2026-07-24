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

// testAPIWithToken mounts the router over a real DB with one profile and returns
// the handler plus a full-access token for it.
func testAPIWithToken(t *testing.T) (http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "api.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	ctx := context.Background()
	if err := database.CreateAccount(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateProfile(ctx, "Ada", ""); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(database, []byte("test-secret-please-ignore-0123456789"))
	_, _, pair, err := authSvc.IssueSession(ctx, auth.Device{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	a := New(Deps{DB: database, Auth: authSvc, Cfg: &config.Config{}, ConfigPath: filepath.Join(dir, "config.toml")})
	r := chi.NewRouter()
	a.Mount(r)
	return r, pair.AccessToken
}

// A getting-past-auth result. The media handlers 404 when the file doesn't
// exist (there's no media in this DB), so a 404 means the request cleared the
// middleware and the per-file check - which is exactly what we're asserting,
// without needing a real media file or ffmpeg.
const pastAuth = http.StatusNotFound

// TestStreamTokenRouteAndScope pins the coupled behavior of the media auth path:
// the stream token mints, works on the media routes (via ?access_token=) only
// for its own file, and is refused everywhere else.
func TestStreamTokenRouteAndScope(t *testing.T) {
	h, access := testAPIWithToken(t)

	// Mint a stream token bound to file 5.
	var tok struct {
		Token string `json:"token"`
	}
	if c := do(t, h, http.MethodGet, "/api/media/5/stream-token", access, nil, &tok); c != http.StatusOK {
		t.Fatalf("stream-token mint = %d, want 200", c)
	}
	if tok.Token == "" {
		t.Fatal("no token minted")
	}

	cases := []struct {
		name string
		path string
		tok  string // bearer header
		want int
	}{
		// A stream token must never authenticate a normal, bearer-only route.
		{"stream token on /api/me", "/api/me", tok.Token, http.StatusUnauthorized},

		// On the media route, the token rides ?access_token=. Right file clears
		// auth (then 404s on the missing file); wrong file is refused.
		{"stream token, right file", "/api/media/5/stream?video=h264&audio=aac&access_token=" + tok.Token, "", pastAuth},
		{"stream token, wrong file", "/api/media/9/stream?video=h264&audio=aac&access_token=" + tok.Token, "", http.StatusForbidden},

		// No token at all on a media byte route is unauthorized.
		{"no token", "/api/media/5/stream?video=h264&audio=aac", "", http.StatusUnauthorized},

		// A full-access token also works on the media route and is not file-bound.
		{"access token, any file", "/api/media/9/stream?video=h264&audio=aac&access_token=" + access, "", pastAuth},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if c := do(t, h, http.MethodGet, tc.path, tc.tok, nil, nil); c != tc.want {
				t.Errorf("%s = %d, want %d", tc.name, c, tc.want)
			}
		})
	}
}
