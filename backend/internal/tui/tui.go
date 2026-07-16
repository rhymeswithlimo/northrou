// Package tui implements Northrou's admin dashboard: a cross-platform Bubble
// Tea terminal UI that connects to the running daemon's local admin API and
// shows active streams, hardware acceleration, capacity, and scan/library
// status. It never touches the database directly.
//
// The dashboard is otherwise a read-only view, with one exception: the Library
// tab is where media folders are configured, by editing config.toml on disk
// (see volumes.go). That is deliberate, and it is why the exception exists here
// rather than in the settings UI -- folders are a property of the server's own
// filesystem. Editing is offered only when the TUI is talking to the box it is
// running on; `northrou admin --addr <remote>` gets the read-only dashboard,
// since this process's config.toml is not that server's.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type view int

const (
	viewLogin view = iota
	viewDashboard
)

// login sub-steps: enter the account email, then the emailed pin.
const (
	stepEmail = iota
	stepPin
)

var tabs = []string{"Streams", "Hardware", "Library"}

// Tab indices, named so the key handling doesn't compare against bare ints.
const (
	tabStreams = iota
	tabHardware
	tabLibrary
)

type model struct {
	client *client
	addr   string
	state  view

	// login
	inputs    []textinput.Model
	loginStep int
	loginErr  string
	busy      bool

	// dashboard
	tab        int
	data       dashboardData
	lastUpdate time.Time
	width      int
	height     int

	// library folders. Editable only when this TUI runs on the box it is
	// showing (see the package doc); otherwise the list is not even loaded.
	store    mediaStore
	local    bool
	volumes  []volume
	volCur   int
	volInput textinput.Model
	adding   bool
	addKind  string
	volErr   string
	volMsg   string
}

// messages
type pinRequestedMsg struct{ err error }
type loginResultMsg struct{ err error }
type dataMsg struct{ data dashboardData }
type tickMsg time.Time

// Run starts the admin TUI against the server at base (e.g.
// http://localhost:8674). configPath is this machine's config.toml, and local
// says whether base is the server this process is running on -- only then may
// the Library tab edit media folders, since configPath describes this box and
// no other.
func Run(base, configPath string, local bool) error {
	m := newModel(base, configPath, local)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func newModel(base, configPath string, local bool) model {
	emailIn := textinput.New()
	emailIn.Placeholder = "you@example.com"
	emailIn.Focus()
	emailIn.CharLimit = 254
	emailIn.Prompt = "› "

	pinIn := textinput.New()
	pinIn.Placeholder = "6-digit code"
	pinIn.CharLimit = 6
	pinIn.Prompt = "› "

	volIn := textinput.New()
	volIn.Placeholder = "/Volumes/Media/Movies"
	volIn.CharLimit = 4096
	volIn.Prompt = "› "

	return model{
		client:    newClient(base),
		addr:      base,
		state:     viewLogin,
		loginStep: stepEmail,
		inputs:    []textinput.Model{emailIn, pinIn},
		store:     mediaStore{path: configPath},
		local:     local,
		volInput:  volIn,
	}
}

// reloadVolumes re-reads the folder list from disk. Called on sign-in and after
// every edit, so the list always reflects the file rather than our idea of it.
func (m *model) reloadVolumes() {
	if !m.local {
		return
	}
	vols, err := m.store.load()
	if err != nil {
		m.volErr = err.Error()
		return
	}
	m.volumes = vols
	if m.volCur >= len(vols) {
		m.volCur = max(len(vols)-1, 0)
	}
}

func (m model) Init() tea.Cmd { return textinput.Blink }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}
		if m.state == viewLogin {
			return m.updateLogin(msg)
		}
		return m.updateDashboard(msg)

	case pinRequestedMsg:
		m.busy = false
		if msg.err != nil {
			m.loginErr = msg.err.Error()
			return m, nil
		}
		// Advance to the pin entry step.
		m.loginStep = stepPin
		m.loginErr = ""
		m.inputs[stepEmail].Blur()
		m.inputs[stepPin].Focus()
		return m, textinput.Blink

	case loginResultMsg:
		m.busy = false
		if msg.err != nil {
			m.loginErr = msg.err.Error()
			return m, nil
		}
		m.state = viewDashboard
		m.reloadVolumes()
		return m, tea.Batch(m.fetchCmd(), tickCmd())

	case dataMsg:
		m.data = msg.data
		m.lastUpdate = time.Now()
		return m, nil

	case tickMsg:
		if m.state == viewDashboard {
			return m, tea.Batch(m.fetchCmd(), tickCmd())
		}
		return m, nil
	}

	// Delegate to the active login input.
	if m.state == viewLogin {
		var cmd tea.Cmd
		m.inputs[m.loginStep], cmd = m.inputs[m.loginStep].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) updateLogin(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if m.busy {
			return m, nil
		}
		m.busy = true
		m.loginErr = ""
		if m.loginStep == stepEmail {
			return m, m.requestPinCmd(strings.TrimSpace(m.inputs[stepEmail].Value()))
		}
		return m, m.verifyPinCmd(
			strings.TrimSpace(m.inputs[stepEmail].Value()),
			strings.TrimSpace(m.inputs[stepPin].Value()))
	case "esc":
		// Back to email entry to correct a typo or resend.
		if m.loginStep == stepPin {
			m.loginStep = stepEmail
			m.loginErr = ""
			m.inputs[stepPin].Blur()
			m.inputs[stepPin].SetValue("")
			m.inputs[stepEmail].Focus()
			return m, textinput.Blink
		}
	}
	var cmd tea.Cmd
	m.inputs[m.loginStep], cmd = m.inputs[m.loginStep].Update(msg)
	return m, cmd
}

func (m model) updateDashboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While typing a folder path, every key belongs to the input. Checked first
	// or "q" would quit and "l" would switch tabs mid-path.
	if m.adding {
		return m.updateAddVolume(msg)
	}
	if m.tab == tabLibrary && m.local {
		if handled, mm, cmd := m.updateVolumeKeys(msg); handled {
			return mm, cmd
		}
	}
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "tab", "right", "l":
		m.tab = (m.tab + 1) % len(tabs)
	case "shift+tab", "left", "h":
		m.tab = (m.tab - 1 + len(tabs)) % len(tabs)
	case "r":
		return m, m.fetchCmd()
	}
	return m, nil
}

// updateVolumeKeys handles the Library tab's folder bindings. It reports
// whether it consumed the key, so anything it ignores still reaches the shared
// tab/quit bindings.
func (m model) updateVolumeKeys(msg tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	switch msg.String() {
	case "m", "t":
		m.adding = true
		m.addKind = kindMovie
		if msg.String() == "t" {
			m.addKind = kindShow
		}
		m.volErr, m.volMsg = "", ""
		m.volInput.SetValue("")
		m.volInput.Focus()
		return true, m, textinput.Blink

	case "up", "k":
		if m.volCur > 0 {
			m.volCur--
		}
		return true, m, nil

	case "down", "j":
		if m.volCur < len(m.volumes)-1 {
			m.volCur++
		}
		return true, m, nil

	case "d", "x", "delete":
		if m.volCur >= len(m.volumes) {
			return true, m, nil
		}
		v := m.volumes[m.volCur]
		m.volErr, m.volMsg = "", ""
		if err := m.store.remove(v.Kind, v.Path); err != nil {
			m.volErr = err.Error()
			return true, m, nil
		}
		m.reloadVolumes()
		// Say the files are safe. "Remove" next to a folder path is exactly
		// where someone fears they just deleted their collection.
		m.volMsg = "Removed " + v.Path + ". The files on disk are untouched."
		return true, m, nil
	}
	return false, m, nil
}

// updateAddVolume owns the keyboard while a path is being typed.
func (m model) updateAddVolume(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.adding = false
		m.volInput.Blur()
		m.volErr = ""
		return m, nil
	case "enter":
		dir := strings.TrimSpace(m.volInput.Value())
		if err := m.store.add(m.addKind, dir); err != nil {
			// Stay in the input with the text intact: the path is nearly right
			// and retyping it from scratch would be punishment.
			m.volErr = err.Error()
			return m, nil
		}
		m.adding = false
		m.volInput.Blur()
		m.volErr = ""
		m.reloadVolumes()
		m.volMsg = "Added " + dir + ". Run a scan to pick up what's inside."
		return m, nil
	}
	var cmd tea.Cmd
	m.volInput, cmd = m.volInput.Update(msg)
	return m, cmd
}

// commands

func (m model) requestPinCmd(email string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		return pinRequestedMsg{err: c.requestPin(ctx, email)}
	}
}

func (m model) verifyPinCmd(email, pin string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		return loginResultMsg{err: c.verifyPin(ctx, email, pin)}
	}
}

func (m model) fetchCmd() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return dataMsg{data: c.fetchAll(ctx)}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// styles (theme-adaptive so they work in light and dark terminals)
var (
	accent   = lipgloss.AdaptiveColor{Light: "#4f46e5", Dark: "#818cf8"}
	subtle   = lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#9ca3af"}
	good     = lipgloss.AdaptiveColor{Light: "#15803d", Dark: "#4ade80"}
	warn     = lipgloss.AdaptiveColor{Light: "#b45309", Dark: "#fbbf24"}

	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(accent)
	subtleStyle  = lipgloss.NewStyle().Foreground(subtle)
	activeTab    = lipgloss.NewStyle().Bold(true).Foreground(accent).Underline(true)
	inactiveTab  = lipgloss.NewStyle().Foreground(subtle)
	boxStyle     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).BorderForeground(subtle)
)

func (m model) View() string {
	if m.state == viewLogin {
		return m.viewLogin()
	}
	return m.viewDashboard()
}

func (m model) viewLogin() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Northrou Admin") + "\n")
	b.WriteString(subtleStyle.Render("Connecting to "+m.addr) + "\n\n")

	if m.loginStep == stepEmail {
		b.WriteString("Email\n" + m.inputs[stepEmail].View() + "\n\n")
		if m.busy {
			b.WriteString(subtleStyle.Render("Sending code…") + "\n")
		}
	} else {
		b.WriteString(subtleStyle.Render("Enter the code sent to "+m.inputs[stepEmail].Value()) + "\n\n")
		b.WriteString("Sign-in code\n" + m.inputs[stepPin].View() + "\n\n")
		if m.busy {
			b.WriteString(subtleStyle.Render("Signing in…") + "\n")
		}
	}

	if m.loginErr != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(warn).Render(m.loginErr) + "\n")
	}
	if m.loginStep == stepEmail {
		b.WriteString(subtleStyle.Render("\nenter: email me a code • ctrl+c: quit"))
	} else {
		b.WriteString(subtleStyle.Render("\nenter: sign in • esc: change email • ctrl+c: quit"))
	}
	return boxStyle.Render(b.String())
}

func (m model) viewDashboard() string {
	var header strings.Builder
	header.WriteString(titleStyle.Render("Northrou Admin") + "  ")
	for i, t := range tabs {
		if i == m.tab {
			header.WriteString(activeTab.Render(t))
		} else {
			header.WriteString(inactiveTab.Render(t))
		}
		if i < len(tabs)-1 {
			header.WriteString(subtleStyle.Render(" │ "))
		}
	}

	var body string
	switch m.tab {
	case tabStreams:
		body = m.viewStreams()
	case tabHardware:
		body = m.viewHardware()
	case tabLibrary:
		body = m.viewLibrary()
	}

	status := ""
	if m.data.err != nil {
		status = lipgloss.NewStyle().Foreground(warn).Render("error: " + m.data.err.Error())
	} else if !m.lastUpdate.IsZero() {
		status = subtleStyle.Render("updated " + m.lastUpdate.Format("15:04:05"))
	}

	footer := subtleStyle.Render("tab: switch view • r: refresh • q: quit")
	if m.tab == tabLibrary {
		footer = subtleStyle.Render(m.volumeHelp())
	}
	return header.String() + "\n\n" + boxStyle.Render(body) + "\n" + status + "\n" + footer
}

func (m model) viewStreams() string {
	s := m.data.streams
	if s.Count == 0 {
		return subtleStyle.Render("No active streams.")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d active stream(s)\n\n", s.Count)
	for _, st := range s.Streams {
		loc := "local"
		if st.Remote {
			loc = "remote"
		}
		title := st.Title
		if title == "" {
			title = "(file)"
		}
		line := fmt.Sprintf("%s  %s  %s→%s / %s  [%s, %s]",
			modeBadge(st.Mode), title, st.SourceVideo, st.TargetVideo, st.TargetAudio, st.HWBackend, loc)
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) viewHardware() string {
	h := m.data.hardware
	if !h.FFmpegReady {
		return warnStyle("ffmpeg still initializing…")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Acceleration backend : %s\n", valueStyle(h.Backend))
	fmt.Fprintf(&b, "Available backends   : %s\n", strings.Join(h.Available, ", "))
	fmt.Fprintf(&b, "Active transcodes    : %d\n", h.ActiveTranscodes)
	cap := fmt.Sprintf("%d simultaneous 4K transcode(s)", h.EstimatedCapacity)
	if h.EstimatedCapacity == 0 {
		cap = "software only (4K not real-time)"
	}
	fmt.Fprintf(&b, "Estimated capacity   : %s\n", cap)
	return strings.TrimRight(b.String(), "\n")
}

func (m model) viewLibrary() string {
	sc := m.data.scan
	var b strings.Builder
	fmt.Fprintf(&b, "Movies : %d\n", m.data.movies)
	fmt.Fprintf(&b, "Shows  : %d\n\n", m.data.shows)
	if sc.Running {
		fmt.Fprintf(&b, "%s scanning… %d/%d (matched %d, unmatched %d)\n",
			lipgloss.NewStyle().Foreground(warn).Render("●"), sc.Processed, sc.Total, sc.Matched, sc.Unmatched)
	} else if sc.Total > 0 {
		fmt.Fprintf(&b, "Last scan: %d processed, %d matched, %d unmatched\n", sc.Processed, sc.Matched, sc.Unmatched)
	} else {
		b.WriteString(subtleStyle.Render("No scan has run yet.\n"))
	}
	b.WriteString("\n" + m.viewVolumes())
	return strings.TrimRight(b.String(), "\n")
}

// viewVolumes renders the media-folder list and its editor.
func (m model) viewVolumes() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Media folders") + "\n")

	if !m.local {
		// We never loaded that server's folders -- our config.toml describes
		// this box. Saying "none configured" here would be a claim about a
		// machine we have not read.
		b.WriteString(subtleStyle.Render(
			"Configured on " + m.addr + " itself. Run `northrou admin` there to see or\n" +
				"change them: the paths are on that server's own disks.\n"))
		return b.String()
	}

	if len(m.volumes) == 0 {
		b.WriteString(subtleStyle.Render("None configured. Nothing will be scanned until you add one.\n"))
	}
	for i, v := range m.volumes {
		marker := "  "
		line := fmt.Sprintf("%-9s %s", v.Label(), v.Path)
		// Don't mark a selection while the cursor is parked and typing.
		if i == m.volCur && !m.adding {
			marker = lipgloss.NewStyle().Foreground(accent).Render("› ")
			line = lipgloss.NewStyle().Foreground(accent).Render(line)
		}
		b.WriteString(marker + line + "\n")
	}

	if m.adding {
		label := "movie"
		if m.addKind == kindShow {
			label = "TV show"
		}
		b.WriteString("\nAdd a " + label + " folder (absolute path):\n")
		b.WriteString(m.volInput.View() + "\n")
	}
	if m.volErr != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(warn).Render(m.volErr) + "\n")
	}
	if m.volMsg != "" && !m.adding {
		b.WriteString(subtleStyle.Render(m.volMsg) + "\n")
	}
	return b.String()
}

// volumeHelp is the Library tab's key legend, which differs from every other
// tab because this is the one place the TUI writes anything.
func (m model) volumeHelp() string {
	if !m.local {
		return "tab: switch view • r: refresh • q: quit"
	}
	if m.adding {
		return "enter: save • esc: cancel"
	}
	return "m: add movies • t: add shows • d: remove • ↑/↓: select • tab: switch view • q: quit"
}

func modeBadge(mode string) string {
	var c lipgloss.AdaptiveColor
	switch mode {
	case "direct":
		c = good
	case "remux", "audio":
		c = accent
	default:
		c = warn
	}
	return lipgloss.NewStyle().Foreground(c).Bold(true).Render(fmt.Sprintf("[%s]", mode))
}

func valueStyle(s string) string { return lipgloss.NewStyle().Foreground(good).Render(s) }
func warnStyle(s string) string  { return lipgloss.NewStyle().Foreground(warn).Render(s) }
