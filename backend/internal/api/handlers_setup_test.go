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

	authSvc := auth.NewService(database, []byte("test-secret-please-ignore-0123456789"), &recordMailer{})
	a := New(Deps{
		DB:         database,
		Auth:       authSvc,
		Cfg:        &config.Config{}, // daemon booted with empty media
		ConfigPath: cfgPath,
	})
	r := chi.NewRouter()
	a.Mount(r)

	code := do(t, r, http.MethodPost, "/api/setup/complete", "", map[string]any{
		"email":         "ada@example.com",
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
