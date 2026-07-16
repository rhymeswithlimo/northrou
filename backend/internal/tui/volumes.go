package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/rhymeswithlimo/northrou/backend/internal/config"
)

// Media folder kinds. These name the config.toml fields they map to.
const (
	kindMovie = "movie"
	kindShow  = "show"
)

// volume is one configured library folder.
type volume struct {
	Path string
	Kind string // kindMovie | kindShow
}

// Label returns the human name for the folder's kind.
func (v volume) Label() string {
	if v.Kind == kindShow {
		return "TV Shows"
	}
	return "Movies"
}

// mediaStore reads and writes the [media] section of config.toml.
//
// This is the only writer of media folders. They are deliberately not settable
// over the API: a media folder is a claim about the server's own filesystem, so
// it can only be typed correctly by someone looking at that filesystem, and a
// client able to rewrite it could point the scanner at any directory the daemon
// can read. Editing here means we can also stat the path and reject a typo up
// front instead of reporting an empty library an hour later.
//
// The daemon may write this same file (the settings API persists its own
// fields), so every operation is a fresh read-modify-write and the last writer
// wins. That is fine for a single household: the two writers touch disjoint
// sections, and the daemon re-reads [media] from disk rather than caching it.
type mediaStore struct{ path string }

// load returns the currently configured folders, movies first.
func (s mediaStore) load() ([]volume, error) {
	cfg, err := s.read()
	if err != nil {
		return nil, err
	}
	var out []volume
	for _, p := range cfg.Media.MovieDirs {
		out = append(out, volume{Path: p, Kind: kindMovie})
	}
	for _, p := range cfg.Media.ShowDirs {
		out = append(out, volume{Path: p, Kind: kindShow})
	}
	return out, nil
}

// read loads config.toml, tolerating a not-yet-created file so the operator can
// add folders before ever running setup.
func (s mediaStore) read() (*config.Config, error) {
	cfg, err := config.Load(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return config.Default(), nil
	}
	return cfg, err
}

// add validates dir and appends it to the given kind.
func (s mediaStore) add(kind, dir string) error {
	dir, err := validateDir(dir)
	if err != nil {
		return err
	}
	cfg, err := s.read()
	if err != nil {
		return err
	}
	target := &cfg.Media.MovieDirs
	if kind == kindShow {
		target = &cfg.Media.ShowDirs
	}
	if slices.Contains(*target, dir) {
		return errors.New("that folder is already in the library")
	}
	*target = append(*target, dir)
	return cfg.Save(s.path)
}

// remove drops dir from the given kind. A folder that is not there is not an
// error: the end state is what was asked for.
func (s mediaStore) remove(kind, dir string) error {
	cfg, err := s.read()
	if err != nil {
		return err
	}
	target := &cfg.Media.MovieDirs
	if kind == kindShow {
		target = &cfg.Media.ShowDirs
	}
	*target = slices.DeleteFunc(*target, func(p string) bool { return p == dir })
	return cfg.Save(s.path)
}

// validateDir cleans dir and checks the server can actually scan it. Running on
// the box is the whole point of setting folders here, so use it: an absolute,
// existing, readable directory is checkable now rather than a mystery later.
func validateDir(dir string) (string, error) {
	if dir == "" {
		return "", errors.New("enter a folder path")
	}
	// A relative path is ambiguous: the daemon's working directory is set by
	// the service manager, not by whoever typed this.
	if !filepath.IsAbs(dir) {
		return "", errors.New("use an absolute path, e.g. /Volumes/Media/Movies")
	}
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("no such folder: %s", dir)
	}
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a folder: %s", dir)
	}
	return dir, nil
}
