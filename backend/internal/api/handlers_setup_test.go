package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rhymeswithlimo/northrou/backend/internal/auth"
	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

// Setup must not erase media folders the TUI wrote to config.toml before the
// browser flow ran. The daemon boots with those folders absent from its
// in-memory config, so saving that copy blind would clobber them -- the same
// staleness the PATCH and scan paths guard against, and easy to reintroduce by
// removing the media assignment from the setup payload (which is exactly what
// moving folders to the TUI did).
func TestSetupPreservesTUIMediaFolders(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	// A folder already on disk, as `northrou admin` would have written it,
	// while the running daemon's config has none.
	onDisk := config.Default()
	onDisk.Media.MovieDirs = []string{"/tank/Movies"}
	onDisk.Media.ShowDirs = []string{"/tank/TV"}
	if err := onDisk.Save(cfgPath); err != nil {
		t.Fatal(err)
	}

	database, err := db.Open(filepath.Join(dir, "api.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	authSvc := auth.NewService(database, []byte("test-secret-please-ignore-0123456789"))
	a := New(Deps{
		DB:         database,
		Auth:       authSvc,
		Cfg:        &config.Config{}, // daemon booted with empty media
		ConfigPath: cfgPath,
	})
	r := chi.NewRouter()
	a.Mount(r)

	code := do(t, r, http.MethodPost, "/api/setup/complete", "", map[string]any{
		"profile_name":  "Ada",
		"enable_remote": true,
	}, nil)
	if code != http.StatusCreated {
		t.Fatalf("setup/complete: got %d, want 201", code)
	}

	got, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Media.MovieDirs) != 1 || got.Media.MovieDirs[0] != "/tank/Movies" {
		t.Errorf("movie_dirs = %v, want [/tank/Movies] -- setup clobbered TUI folders", got.Media.MovieDirs)
	}
	if len(got.Media.ShowDirs) != 1 || got.Media.ShowDirs[0] != "/tank/TV" {
		t.Errorf("show_dirs = %v, want [/tank/TV]", got.Media.ShowDirs)
	}
	// The account was still created -- the fix preserves media, it doesn't skip setup.
	if exists, _ := database.AccountExists(context.Background()); !exists {
		t.Error("account was not created")
	}
}

// The TUI wizard submits everything through /api/setup/complete: the server
// name and its media folders must land in the daemon's config file, folders
// merged with (not replacing) any already on disk, and unreadable paths must
// be rejected before any state changes.
func TestSetupPersistsNameAndFolders(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	onDisk := config.Default()
	onDisk.Media.MovieDirs = []string{"/tank/Movies"}
	if err := onDisk.Save(cfgPath); err != nil {
		t.Fatal(err)
	}

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
		ConfigPath: cfgPath,
	})
	r := chi.NewRouter()
	a.Mount(r)

	// A folder that does not exist fails setup up front, atomically.
	code := do(t, r, http.MethodPost, "/api/setup/complete", "", map[string]any{
		"server_name": "Attic",
		"movie_dirs":  []string{filepath.Join(dir, "not-there")},
	}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("bad folder: got %d, want 400", code)
	}
	if exists, _ := database.AccountExists(context.Background()); exists {
		t.Fatal("account must not be created when validation fails")
	}

	// Valid folders: name persisted, wizard folders merged with the on-disk one.
	movies := filepath.Join(dir, "media", "movies")
	shows := filepath.Join(dir, "media", "tv")
	for _, d := range []string{movies, shows} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var resp struct {
		ConnectionCode string `json:"connection_code"`
	}
	code = do(t, r, http.MethodPost, "/api/setup/complete", "", map[string]any{
		"server_name":   "Attic",
		"enable_remote": true,
		"movie_dirs":    []string{movies},
		"show_dirs":     []string{shows},
	}, &resp)
	if code != http.StatusCreated {
		t.Fatalf("setup/complete: got %d, want 201", code)
	}
	if resp.ConnectionCode == "" {
		t.Error("expected a connection code in the response")
	}

	got, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Server.Name != "Attic" {
		t.Errorf("server name = %q, want Attic", got.Server.Name)
	}
	wantMovies := []string{"/tank/Movies", movies}
	if len(got.Media.MovieDirs) != 2 || got.Media.MovieDirs[0] != wantMovies[0] || got.Media.MovieDirs[1] != wantMovies[1] {
		t.Errorf("movie_dirs = %v, want %v", got.Media.MovieDirs, wantMovies)
	}
	if len(got.Media.ShowDirs) != 1 || got.Media.ShowDirs[0] != shows {
		t.Errorf("show_dirs = %v, want [%s]", got.Media.ShowDirs, shows)
	}
}
