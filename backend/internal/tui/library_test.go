package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rhymeswithlimo/northrou/backend/internal/config"
)

// Drive the Library tab the way an operator would: add a folder by keystroke,
// see it listed, remove it.
func TestLibraryTabAddsAndRemovesFolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Default().Save(path); err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()

	m := newModel("http://localhost:8674", path, true)
	m.state = viewDashboard
	m.tab = tabLibrary

	// "m" opens the add-a-movie-folder input.
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = mm.(model)
	if !m.adding || m.addKind != kindMovie {
		t.Fatalf("expected movie add mode, got adding=%v kind=%q", m.adding, m.addKind)
	}

	// Type the path, then Enter.
	for _, r := range media {
		mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mm.(model)
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(model)

	if m.adding {
		t.Fatalf("still adding; volErr=%q", m.volErr)
	}
	if m.volErr != "" {
		t.Fatalf("unexpected error: %q", m.volErr)
	}
	if len(m.volumes) != 1 || m.volumes[0].Path != media {
		t.Fatalf("volumes = %+v, want the added folder", m.volumes)
	}
	if !strings.Contains(m.View(), media) {
		t.Errorf("Library view does not show the folder:\n%s", m.View())
	}
	// It must be on disk, not just in memory: the daemon reads the file.
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Media.MovieDirs) != 1 || cfg.Media.MovieDirs[0] != media {
		t.Errorf("config.toml movie_dirs = %v, want [%s]", cfg.Media.MovieDirs, media)
	}

	// "d" removes the selected folder.
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = mm.(model)
	if len(m.volumes) != 0 {
		t.Fatalf("volumes = %+v, want empty after remove", m.volumes)
	}
	cfg, _ = config.Load(path)
	if len(cfg.Media.MovieDirs) != 0 {
		t.Errorf("config.toml still has %v", cfg.Media.MovieDirs)
	}
}

// A bad path must not be silently accepted, and must leave the operator in the
// input with their text, not back at square one.
func TestLibraryTabRejectsBadPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	_ = config.Default().Save(path)

	m := newModel("http://localhost:8674", path, true)
	m.state = viewDashboard
	m.tab = tabLibrary

	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = mm.(model)
	for _, r := range "relative/path" {
		mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mm.(model)
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(model)

	if !m.adding {
		t.Error("expected to stay in the input after a bad path")
	}
	if !strings.Contains(m.volErr, "absolute") {
		t.Errorf("volErr = %q, want it to mention absolute paths", m.volErr)
	}
	if len(m.volumes) != 0 {
		t.Errorf("bad path was stored: %+v", m.volumes)
	}
}

// Pointed at another box, the tab must not offer to edit this box's config.
func TestLibraryTabReadOnlyWhenRemote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	_ = config.Default().Save(path)
	m := newModel("http://nas.example:8674", path, false)
	m.state = viewDashboard
	m.tab = tabLibrary

	// "m" must not open an editor.
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = mm.(model)
	if m.adding {
		t.Error("remote dashboard must not allow adding folders")
	}
	out := m.View()
	if !strings.Contains(out, "nas.example") {
		t.Errorf("view should name the remote server:\n%s", out)
	}
}
