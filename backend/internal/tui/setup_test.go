package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// wizardServer mocks the endpoints the setup wizard drives, capturing what
// /api/setup/complete receives.
func wizardServer(t *testing.T, needsSetup bool) (*httptest.Server, *setupRequest, *bool) {
	t.Helper()
	var got setupRequest
	scanStarted := false

	mux := http.NewServeMux()
	mux.HandleFunc("/api/setup/status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"needs_setup": needsSetup})
	})
	mux.HandleFunc("/api/setup/complete", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"connection_code":"NR-ABCDE-FGHJK","access_token":"tok","refresh_token":"r","profile":{"id":1,"name":"Me"}}`))
	})
	mux.HandleFunc("/api/admin/scan", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if r.Header.Get("Authorization") != "Bearer tok" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			scanStarted = true
		}
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/api/auth/pair", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"tok","profile":{"id":1,"name":"Me"},"server_name":"Attic"}`))
	})
	mux.HandleFunc("/api/admin/config", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"server_name":"Attic","connection_code":"NR-ABCDE-FGHJK","remote_enabled":true}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &got, &scanStarted
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// press applies one key to the model and runs any command it returns, feeding
// wizard-flow messages back in - enough to drive the wizard's synchronous
// transitions plus its submit round-trip. Anything else a command produces
// (cursor blinks, batched dashboard ticks) is dropped rather than followed.
func press(t *testing.T, m model, k tea.KeyMsg) model {
	t.Helper()
	next, cmd := m.Update(k)
	m = next.(model)
	for cmd != nil {
		switch msg := cmd().(type) {
		case setupStatusMsg, setupDoneMsg, infoMsg, pairedMsg, dataMsg:
			next, cmd = m.Update(msg)
			m = next.(model)
		default:
			cmd = nil
		}
	}
	return m
}

// typeString feeds runes without executing the returned commands: a textinput
// answers every keystroke with a ~half-second cursor-blink command, and
// running two dozen of those would turn this into a wall-clock test.
func typeString(t *testing.T, m model, s string) model {
	t.Helper()
	for _, r := range s {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(model)
	}
	return m
}

// newSetupModel builds a setup-mode model wired at the mock server, past the
// initial status check (which needs setup).
func newSetupModel(t *testing.T, srv *httptest.Server) model {
	t.Helper()
	m := newModel(srv.URL, filepath.Join(t.TempDir(), "config.toml"), true)
	m.setupMode = true
	m.wiz = newWizard(nil)

	msg := m.Init()()
	next, _ := m.Update(msg)
	m = next.(model)
	if m.state != viewWizard {
		t.Fatalf("expected wizard after needs-setup status, got state %d (err %q)", m.state, m.loginErr)
	}
	return m
}

func TestWizardWalkthrough(t *testing.T) {
	srv, got, scanStarted := wizardServer(t, true)
	m := newSetupModel(t, srv)

	// Step 1: name. Clear the hostname prefill and type our own.
	m.wiz.name.SetValue("")
	m = typeString(t, m, "Attic Server")
	m = press(t, m, key("enter"))
	if m.wiz.step != wizFolders {
		t.Fatalf("expected folders step, got %d", m.wiz.step)
	}

	// Step 2: add one movie folder, then continue.
	dir := t.TempDir()
	m = press(t, m, key("m"))
	if !m.wiz.adding {
		t.Fatal("expected folder input to open")
	}
	m.wiz.folderInput.SetValue(dir)
	m = press(t, m, key("enter"))
	if m.wiz.adding || len(m.wiz.folders) != 1 {
		t.Fatalf("expected one folder collected, got %+v (adding=%v, err=%q)", m.wiz.folders, m.wiz.adding, m.wiz.errMsg)
	}
	m = press(t, m, key("enter"))
	if m.wiz.step != wizTMDB {
		t.Fatalf("expected TMDB step, got %d", m.wiz.step)
	}

	// Step 3: TMDB key.
	m = typeString(t, m, "tmdb-key-123")
	m = press(t, m, key("enter"))
	if m.wiz.step != wizRemote {
		t.Fatalf("expected remote step, got %d", m.wiz.step)
	}

	// Step 4: leave remote on and finish. press() runs the submit command and
	// feeds setupDoneMsg back in.
	if !m.wiz.remoteOn {
		t.Fatal("remote should default to on")
	}
	m = press(t, m, key("enter"))

	if m.state != viewSummary {
		t.Fatalf("expected summary after submit, got state %d (wizard err %q)", m.state, m.wiz.errMsg)
	}
	if got.ServerName != "Attic Server" || got.TMDBAPIKey != "tmdb-key-123" || !got.EnableRemote {
		t.Errorf("unexpected setup payload: %+v", *got)
	}
	if len(got.MovieDirs) != 1 || got.MovieDirs[0] != dir {
		t.Errorf("expected movie dir %q, got %v", dir, got.MovieDirs)
	}
	if !*scanStarted {
		t.Error("expected a scan to be kicked off after setup with folders")
	}
	if !m.sum.celebrate || m.sum.code != "NR-ABCDE-FGHJK" {
		t.Errorf("unexpected summary: %+v", m.sum)
	}

	out := m.View()
	for _, want := range []string{"Attic Server", "NR-ABCDE-FGHJK", "app.northrou.sh", "scanned"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary view missing %q:\n%s", want, out)
		}
	}

	// Enter opens the dashboard.
	next, _ := m.Update(key("enter"))
	if next.(model).state != viewDashboard {
		t.Error("expected enter on summary to open the dashboard")
	}
}

func TestWizardRejectsBadFolder(t *testing.T) {
	srv, _, _ := wizardServer(t, true)
	m := newSetupModel(t, srv)
	m = press(t, m, key("enter")) // accept name

	m = press(t, m, key("t"))
	m.wiz.folderInput.SetValue(filepath.Join(os.TempDir(), "definitely-not-there-xyz"))
	m = press(t, m, key("enter"))
	if !m.wiz.adding || m.wiz.errMsg == "" {
		t.Errorf("expected to stay in the input with an error, got adding=%v err=%q", m.wiz.adding, m.wiz.errMsg)
	}
	if len(m.wiz.folders) != 0 {
		t.Errorf("bad folder must not be collected: %+v", m.wiz.folders)
	}
}

func TestWizardBackNavigation(t *testing.T) {
	srv, _, _ := wizardServer(t, true)
	m := newSetupModel(t, srv)
	m = press(t, m, key("enter")) // -> folders
	m = press(t, m, key("enter")) // -> tmdb
	m = press(t, m, key("esc"))   // back to folders
	if m.wiz.step != wizFolders {
		t.Fatalf("expected back to folders, got %d", m.wiz.step)
	}
	m = press(t, m, key("esc")) // back to name
	if m.wiz.step != wizName {
		t.Fatalf("expected back to name, got %d", m.wiz.step)
	}
}

func TestSetupModeAlreadyConfigured(t *testing.T) {
	srv, _, _ := wizardServer(t, false)
	m := newModel(srv.URL, filepath.Join(t.TempDir(), "config.toml"), true)
	m.setupMode = true
	m.wiz = newWizard(nil)

	// status (not needed) -> pair -> info -> summary
	msg := m.Init()()
	m = press(t, m, key("x")) // no-op key; drive via explicit messages instead
	next, cmd := m.Update(msg)
	m = next.(model)
	for cmd != nil {
		out := cmd()
		if out == nil {
			break
		}
		if _, ok := out.(tea.BatchMsg); ok {
			break
		}
		next, cmd = m.Update(out)
		m = next.(model)
	}

	if m.state != viewSummary {
		t.Fatalf("expected summary for an already-set-up box, got state %d", m.state)
	}
	if m.sum.celebrate {
		t.Error("re-running setup should not celebrate")
	}
	if m.sum.serverName != "Attic" || m.sum.code != "NR-ABCDE-FGHJK" {
		t.Errorf("unexpected summary: %+v", m.sum)
	}
	if out := m.View(); !strings.Contains(out, "already set up") {
		t.Errorf("summary should say already set up:\n%s", out)
	}
}
