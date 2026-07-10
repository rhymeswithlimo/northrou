// Package tui implements Northrou's admin dashboard: a cross-platform Bubble
// Tea terminal UI that connects to the running daemon's local admin API and
// shows active streams, hardware acceleration, capacity, and scan/library
// status. It is a read-only client that never touches the database directly.
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

var tabs = []string{"Streams", "Hardware", "Library"}

type model struct {
	client *client
	addr   string
	state  view

	// login
	inputs   []textinput.Model
	focus    int
	loginErr string
	loggingIn bool

	// dashboard
	tab        int
	data       dashboardData
	lastUpdate time.Time
	width      int
	height     int
}

// messages
type loginResultMsg struct{ err error }
type dataMsg struct{ data dashboardData }
type tickMsg time.Time

// Run starts the admin TUI against the server at base (e.g. http://localhost:8674).
func Run(base string) error {
	m := newModel(base)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func newModel(base string) model {
	user := textinput.New()
	user.Placeholder = "admin username"
	user.Focus()
	user.CharLimit = 64
	user.Prompt = "› "

	pass := textinput.New()
	pass.Placeholder = "password"
	pass.EchoMode = textinput.EchoPassword
	pass.CharLimit = 128
	pass.Prompt = "› "

	return model{
		client: newClient(base),
		addr:   base,
		state:  viewLogin,
		inputs: []textinput.Model{user, pass},
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

	case loginResultMsg:
		m.loggingIn = false
		if msg.err != nil {
			m.loginErr = msg.err.Error()
			return m, nil
		}
		m.state = viewDashboard
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

	// Delegate to focused text input on the login screen.
	if m.state == viewLogin {
		var cmd tea.Cmd
		m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) updateLogin(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab", "down":
		m.focus = (m.focus + 1) % len(m.inputs)
		m.applyFocus()
		return m, textinput.Blink
	case "shift+tab", "up":
		m.focus = (m.focus - 1 + len(m.inputs)) % len(m.inputs)
		m.applyFocus()
		return m, textinput.Blink
	case "enter":
		if m.loggingIn {
			return m, nil
		}
		m.loggingIn = true
		m.loginErr = ""
		return m, m.loginCmd(m.inputs[0].Value(), m.inputs[1].Value())
	}
	var cmd tea.Cmd
	m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
	return m, cmd
}

func (m *model) applyFocus() {
	for i := range m.inputs {
		if i == m.focus {
			m.inputs[i].Focus()
		} else {
			m.inputs[i].Blur()
		}
	}
}

func (m model) updateDashboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

// commands

func (m model) loginCmd(user, pass string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		return loginResultMsg{err: c.login(ctx, user, pass)}
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
	b.WriteString("Username\n" + m.inputs[0].View() + "\n\n")
	b.WriteString("Password\n" + m.inputs[1].View() + "\n\n")
	if m.loggingIn {
		b.WriteString(subtleStyle.Render("Signing in…") + "\n")
	}
	if m.loginErr != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(warn).Render(m.loginErr) + "\n")
	}
	b.WriteString(subtleStyle.Render("\ntab: switch field • enter: sign in • ctrl+c: quit"))
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
	case 0:
		body = m.viewStreams()
	case 1:
		body = m.viewHardware()
	case 2:
		body = m.viewLibrary()
	}

	status := ""
	if m.data.err != nil {
		status = lipgloss.NewStyle().Foreground(warn).Render("error: " + m.data.err.Error())
	} else if !m.lastUpdate.IsZero() {
		status = subtleStyle.Render("updated " + m.lastUpdate.Format("15:04:05"))
	}

	footer := subtleStyle.Render("tab: switch view • r: refresh • q: quit")
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
	return strings.TrimRight(b.String(), "\n")
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
