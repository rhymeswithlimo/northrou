package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/config"
)

// newStore returns a store over a fresh config.toml containing a valid,
// defaulted config (what a set-up box has).
func newStore(t *testing.T) mediaStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Default().Save(path); err != nil {
		t.Fatal(err)
	}
	return mediaStore{path: path}
}

func TestValidateDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "movie.mkv")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr string
	}{
		{name: "absolute existing dir", in: dir, want: dir},
		{name: "cleans trailing slash", in: dir + "/", want: dir},
		{name: "empty", in: "", wantErr: "enter a folder"},
		{name: "relative", in: "media/movies", wantErr: "absolute path"},
		{name: "missing", in: filepath.Join(dir, "nope"), wantErr: "no such folder"},
		{name: "is a file", in: file, wantErr: "not a folder"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateDir(tt.in)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (%q)", tt.wantErr, got)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAddAndRemoveVolumes(t *testing.T) {
	s := newStore(t)
	movies, shows := t.TempDir(), t.TempDir()

	if err := s.add(kindMovie, movies); err != nil {
		t.Fatalf("add movies: %v", err)
	}
	if err := s.add(kindShow, shows); err != nil {
		t.Fatalf("add shows: %v", err)
	}

	got, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	want := []volume{{Path: movies, Kind: kindMovie}, {Path: shows, Kind: kindShow}}
	if len(got) != len(want) {
		t.Fatalf("got %d volumes, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("volume %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	if err := s.remove(kindMovie, movies); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got, _ = s.load()
	if len(got) != 1 || got[0].Kind != kindShow {
		t.Errorf("after remove, got %+v; want only the show folder", got)
	}
	// Removing what is already gone is the requested end state, not an error.
	if err := s.remove(kindMovie, movies); err != nil {
		t.Errorf("removing an absent folder should be a no-op, got %v", err)
	}
}

func TestAddRejectsDuplicate(t *testing.T) {
	s := newStore(t)
	dir := t.TempDir()
	if err := s.add(kindMovie, dir); err != nil {
		t.Fatal(err)
	}
	if err := s.add(kindMovie, dir); err == nil {
		t.Fatal("expected an error adding the same folder twice")
	}
	got, _ := s.load()
	if len(got) != 1 {
		t.Errorf("duplicate was stored: %+v", got)
	}
}

// The daemon persists its own settings to this same file. An edit here must not
// take the rest of the config with it -- that would silently revert whatever the
// settings page last saved.
func TestAddPreservesOtherConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := config.Default()
	cfg.TMDB.APIKey = "key123"
	cfg.Transcode.MaxTranscodes = 3
	cfg.Remote.Enabled = true
	cfg.Remote.ConnectionCode = "NR-ABCD-1234"
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}

	s := mediaStore{path: path}
	if err := s.add(kindMovie, t.TempDir()); err != nil {
		t.Fatal(err)
	}

	got, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.TMDB.APIKey != "key123" {
		t.Errorf("tmdb key lost: %q", got.TMDB.APIKey)
	}
	if got.Transcode.MaxTranscodes != 3 {
		t.Errorf("max_transcodes lost: %d", got.Transcode.MaxTranscodes)
	}
	if !got.Remote.Enabled || got.Remote.ConnectionCode != "NR-ABCD-1234" {
		t.Errorf("remote config lost: %+v", got.Remote)
	}
}

// Folders are addable before setup has ever written a config.toml.
func TestAddWithNoConfigFileYet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	s := mediaStore{path: path}
	dir := t.TempDir()
	if err := s.add(kindMovie, dir); err != nil {
		t.Fatalf("add with no config file: %v", err)
	}
	got, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != dir {
		t.Errorf("got %+v, want the added folder", got)
	}
}
