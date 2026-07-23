// First-run setup wizard: the terminal walk-through `northrou setup` runs on
// the box itself. It collects the server name, media folders, an optional TMDB
// key, and the remote-access choice, then submits everything to the local
// daemon's /api/setup/complete - which writes the daemon's own config.toml and
// hands back a signed-in session plus the connection code. Setup lives here
// rather than in a browser page because it is an operator action on the
// server's own filesystem, and because a headless box reached over SSH is the
// primary deployment, not an edge case.
package tui

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rhymeswithlimo/northrou/backend/internal/setup"
)

// wizStep enumerates the wizard's screens, in order.
type wizStep int

const (
	wizName wizStep = iota
	wizFolders
	wizTMDB
	wizRemote
	wizSubmitting
)

// wizard holds the setup wizard's state. It lives inside the shared model so
// finishing setup can fall through to the regular dashboard.
type wizard struct {
	step wizStep

	name        textinput.Model
	tmdb        textinput.Model
	folderInput textinput.Model

	// folders collected so far. Kept in memory and submitted with the setup
	// request (not written to this process's config.toml) so they land in the
	// daemon's own config file, which may not be the same one.
	folders []volume
	cur     int
	adding  bool
	addKind string

	remoteOn bool
	errMsg   string
}

// summary is the recap screen shown after the wizard finishes, and when
// `northrou setup` is re-run on a box that is already set up.
type summary struct {
	celebrate  bool // fresh setup vs. "already set up"
	serverName string
	code       string
	remoteOn   bool
	scanning   bool // a first scan was kicked off
}

// wizard-flow messages.
type setupStatusMsg struct {
	needsSetup bool
	err        error
}
type setupDoneMsg struct {
	code     string
	scanning bool
	err      error
}
type infoMsg struct {
	info serverInfo
	name string
	err  error
}

// newWizard builds the wizard's inputs. Existing folders (an operator may have
// added some via `northrou admin` before ever running setup) seed the list.
func newWizard(existing []volume) wizard {
	name := textinput.New()
	name.CharLimit = 64
	name.Prompt = "› "
	if host, err := os.Hostname(); err == nil {
		name.SetValue(host)
	}
	name.Focus()

	tmdb := textinput.New()
	tmdb.Placeholder = "paste your TMDB API key, or leave blank"
	tmdb.CharLimit = 128
	tmdb.Prompt = "› "

	folder := textinput.New()
	folder.Placeholder = "/Volumes/Media/Movies"
	folder.CharLimit = 4096
	folder.Prompt = "› "

	return wizard{
		name:        name,
		tmdb:        tmdb,
		folderInput: folder,
		folders:     existing,
		remoteOn:    true,
	}
}

// updateWizard owns the keyboard while the wizard is on screen.
func (m model) updateWizard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	w := &m.wiz

	switch w.step {
	case wizName:
		switch msg.String() {
		case "enter":
			w.step = wizFolders
			return m, nil
		}
		var cmd tea.Cmd
		w.name, cmd = w.name.Update(msg)
		return m, cmd

	case wizFolders:
		return m.updateWizardFolders(msg)

	case wizTMDB:
		switch msg.String() {
		case "enter":
			w.step = wizRemote
			return m, nil
		case "esc":
			w.step = wizFolders
			return m, nil
		}
		var cmd tea.Cmd
		w.tmdb, cmd = w.tmdb.Update(msg)
		return m, cmd

	case wizRemote:
		switch msg.String() {
		case " ", "left", "right", "tab", "h", "l":
			w.remoteOn = !w.remoteOn
		case "y":
			w.remoteOn = true
		case "n":
			w.remoteOn = false
		case "esc":
			w.step = wizTMDB
		case "enter":
			w.step = wizSubmitting
			w.errMsg = ""
			return m, m.submitSetupCmd()
		}
		return m, nil

	case wizSubmitting:
		// Nothing to type; the submit command reports back via setupDoneMsg.
		return m, nil
	}
	return m, nil
}

// updateWizardFolders handles the folder-collection step: a list plus the same
// add/remove flow as the dashboard's Library tab, but held in memory.
func (m model) updateWizardFolders(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	w := &m.wiz

	if w.adding {
		switch msg.String() {
		case "esc":
			w.adding = false
			w.folderInput.Blur()
			w.errMsg = ""
			return m, nil
		case "enter":
			dir, err := validateDir(strings.TrimSpace(w.folderInput.Value()))
			if err != nil {
				// Stay in the input with the text intact: the path is nearly
				// right and retyping it from scratch would be punishment.
				w.errMsg = err.Error()
				return m, nil
			}
			if slices.ContainsFunc(w.folders, func(v volume) bool { return v.Path == dir }) {
				w.errMsg = "that folder is already in the list"
				return m, nil
			}
			w.folders = append(w.folders, volume{Path: dir, Kind: w.addKind})
			w.adding = false
			w.folderInput.Blur()
			w.errMsg = ""
			return m, nil
		}
		var cmd tea.Cmd
		w.folderInput, cmd = w.folderInput.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "m", "t":
		w.adding = true
		w.addKind = kindMovie
		if msg.String() == "t" {
			w.addKind = kindShow
		}
		w.errMsg = ""
		w.folderInput.SetValue("")
		w.folderInput.Focus()
		return m, textinput.Blink
	case "up", "k":
		if w.cur > 0 {
			w.cur--
		}
	case "down", "j":
		if w.cur < len(w.folders)-1 {
			w.cur++
		}
	case "d", "x", "delete":
		if w.cur < len(w.folders) {
			w.folders = slices.Delete(w.folders, w.cur, w.cur+1)
			if w.cur >= len(w.folders) {
				w.cur = max(len(w.folders)-1, 0)
			}
		}
	case "esc":
		w.step = wizName
	case "enter":
		w.step = wizTMDB
		w.errMsg = ""
		return m, w.tmdb.Focus()
	}
	return m, nil
}

// submitSetupCmd sends the collected answers to the local daemon.
func (m model) submitSetupCmd() tea.Cmd {
	c := m.client
	req := setupRequest{
		ServerName:   strings.TrimSpace(m.wiz.name.Value()),
		TMDBAPIKey:   strings.TrimSpace(m.wiz.tmdb.Value()),
		EnableRemote: m.wiz.remoteOn,
	}
	for _, v := range m.wiz.folders {
		if v.Kind == kindShow {
			req.ShowDirs = append(req.ShowDirs, v.Path)
		} else {
			req.MovieDirs = append(req.MovieDirs, v.Path)
		}
	}
	haveFolders := len(m.wiz.folders) > 0

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		code, err := c.setupComplete(ctx, req)
		if err != nil {
			return setupDoneMsg{err: err}
		}
		// With folders configured, kick the first scan right away: the library
		// starts filling while the operator is still reading the recap.
		scanning := false
		if haveFolders {
			scanning = c.startScan(ctx) == nil
		}
		return setupDoneMsg{code: code, scanning: scanning}
	}
}

// commands used by setup mode's boot sequence.

func (m model) statusCmd() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		needs, err := c.setupStatus(ctx)
		return setupStatusMsg{needsSetup: needs, err: err}
	}
}

// infoCmd fetches what the already-set-up summary shows (code, remote state,
// name). Runs after pair, so the client is authenticated.
func (m model) infoCmd() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		info, err := c.fetchServerInfo(ctx)
		return infoMsg{info: info, name: info.ServerName, err: err}
	}
}

// --- views ---

var wizTitles = []string{
	"Name your server",
	"Add your media folders",
	"Movie & TV metadata",
	"Watch away from home",
}

func (m model) viewWizard() string {
	w := m.wiz

	var b strings.Builder
	b.WriteString(titleStyle.Render("Northrou Setup"))
	if int(w.step) < len(wizTitles) {
		fmt.Fprintf(&b, "  %s", subtleStyle.Render(fmt.Sprintf("Step %d of %d", int(w.step)+1, len(wizTitles))))
	}
	b.WriteString("\n\n")

	var body strings.Builder
	switch w.step {
	case wizName:
		body.WriteString(titleStyle.Render(wizTitles[wizName]) + "\n\n")
		body.WriteString("This is what your devices will show when they pair with it.\n\n")
		body.WriteString(w.name.View() + "\n")

	case wizFolders:
		body.WriteString(titleStyle.Render(wizTitles[wizFolders]) + "\n\n")
		body.WriteString("Point Northrou at the folders that hold your movies and TV shows.\n")
		body.WriteString("You can add more later with `northrou admin`.\n\n")
		if len(w.folders) == 0 && !w.adding {
			body.WriteString(subtleStyle.Render("No folders yet. Press m to add a movie folder, t for TV shows.\n"))
		}
		body.WriteString(renderVolumeList(w.folders, w.cur, w.adding))
		if w.adding {
			label := "movie"
			if w.addKind == kindShow {
				label = "TV show"
			}
			body.WriteString("\nAdd a " + label + " folder (absolute path):\n")
			body.WriteString(w.folderInput.View() + "\n")
		}

	case wizTMDB:
		body.WriteString(titleStyle.Render(wizTitles[wizTMDB]) + "\n\n")
		body.WriteString("A TMDB API key fetches posters, descriptions, cast, and artwork\n")
		body.WriteString("for everything in your library. Free at:\n")
		body.WriteString(valueStyle("themoviedb.org/settings/api") + "\n\n")
		body.WriteString("The key stays on this server. Optional - you can add it later.\n\n")
		body.WriteString(w.tmdb.View() + "\n")

	case wizRemote, wizSubmitting:
		body.WriteString(titleStyle.Render(wizTitles[wizRemote]) + "\n\n")
		body.WriteString("Remote access lets your devices reach this server from anywhere,\n")
		body.WriteString("using a connection code. Streaming is peer-to-peer: your media\n")
		body.WriteString("never passes through external servers.\n\n")
		on, off := "( ) Enabled", "( ) Disabled"
		if w.remoteOn {
			on = lipgloss.NewStyle().Foreground(good).Render("(•) Enabled")
		} else {
			off = lipgloss.NewStyle().Foreground(warn).Render("(•) Disabled")
		}
		body.WriteString(on + "    " + off + "\n")
		if w.step == wizSubmitting {
			body.WriteString("\n" + subtleStyle.Render("Setting up your server…") + "\n")
		}
	}

	b.WriteString(boxStyle.Render(strings.TrimRight(body.String(), "\n")))
	b.WriteString("\n")
	if w.errMsg != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(warn).Render(w.errMsg) + "\n")
	}
	b.WriteString(subtleStyle.Render(m.wizardHelp()))
	return b.String()
}

// wizardHelp is the per-step key legend.
func (m model) wizardHelp() string {
	w := m.wiz
	switch w.step {
	case wizName:
		return "enter: continue • ctrl+c: quit"
	case wizFolders:
		if w.adding {
			return "enter: add folder • esc: cancel"
		}
		return "m: add movies • t: add shows • d: remove • ↑/↓: select • enter: continue • esc: back"
	case wizTMDB:
		return "enter: continue (blank to skip) • esc: back"
	case wizRemote:
		return "space: toggle • enter: finish setup • esc: back"
	case wizSubmitting:
		return "ctrl+c: quit"
	}
	return ""
}

// viewSummary is the recap: connection code, addresses, and what happens next.
func (m model) viewSummary() string {
	s := m.sum

	var b strings.Builder
	if s.celebrate {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(good).Render("✓ "+s.serverName+" is ready") + "\n")
	} else {
		b.WriteString(titleStyle.Render(s.serverName) + "  " + subtleStyle.Render("already set up") + "\n")
	}
	b.WriteString("\n")

	var body strings.Builder
	if s.remoteOn && s.code != "" {
		body.WriteString("Connection code\n")
		body.WriteString(codeStyle.Render(s.code) + "\n\n")
		body.WriteString("Enter it in the Northrou app, or at " + valueStyle("app.northrou.sh") + ",\n")
		body.WriteString("to watch from anywhere. Anyone with this code can watch your\n")
		body.WriteString("library, so keep it private and only share it with people you trust.\n")
	} else {
		body.WriteString("Remote access is off: this server is reachable on your home\n")
		body.WriteString("network only. Turn it on any time in Settings.\n")
	}
	body.WriteString("\nOn your network:\n")
	fmt.Fprintf(&body, "  http://localhost:%d/\n", m.port)
	for _, ip := range setup.LocalIPv4s() {
		fmt.Fprintf(&body, "  http://%s:%d/\n", ip, m.port)
	}
	if s.scanning {
		body.WriteString("\n" + valueStyle("Your library is being scanned now.") + "\n")
	}

	b.WriteString(boxStyle.Render(strings.TrimRight(body.String(), "\n")))
	b.WriteString("\n")
	b.WriteString(subtleStyle.Render("enter: open the dashboard • q: quit"))
	return b.String()
}

// renderVolumeList renders a folder list with a selection marker. Shared by
// the dashboard's Library tab and the wizard's folders step.
func renderVolumeList(vols []volume, cur int, adding bool) string {
	var b strings.Builder
	for i, v := range vols {
		marker := "  "
		line := fmt.Sprintf("%-9s %s", v.Label(), v.Path)
		// Don't mark a selection while the cursor is parked and typing.
		if i == cur && !adding {
			marker = lipgloss.NewStyle().Foreground(accent).Render("› ")
			line = lipgloss.NewStyle().Foreground(accent).Render(line)
		}
		b.WriteString(marker + line + "\n")
	}
	return b.String()
}

// codeStyle renders the connection code large and unmistakable.
var codeStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(accent).
	Border(lipgloss.RoundedBorder()).
	BorderForeground(accent).
	Padding(0, 2)
