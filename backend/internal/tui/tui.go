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

// login sub-steps: enter the account email, then the emailed pin.
const (
	stepEmail = iota
	stepPin
)

var tabs = []string{"Streams", "Hardware", "Library"}

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
}

// messages
type pinRequestedMsg struct{ err error }
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
	emailIn := textinput.New()
	emailIn.Placeholder = "you@example.com"
	emailIn.Focus()
	emailIn.CharLimit = 254
	emailIn.Prompt = "› "

	pinIn := textinput.New()
	pinIn.Placeholder = "6-digit code"
	pinIn.CharLimit = 6
	pinIn.Prompt = "› "

	return model{
		client:    newClient(base),
		addr:      base,
		state:     viewLogin,
		loginStep: stepEmail,
		inputs:    []textinput.Model{emailIn, pinIn},
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
