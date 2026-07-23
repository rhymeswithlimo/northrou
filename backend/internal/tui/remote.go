// The dashboard's Remote tab: the connection code, the remote-access switch,
// and the list of paired devices with revocation. This is where the operator's
// control over who can reach the server lives; the server enforces that the
// mutations only work for local (non-tunnel) requests.

package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// remoteActionMsg reports a Remote-tab mutation finishing; msg is shown in the
// tab and a refresh follows so the view reflects the server's new state.
type remoteActionMsg struct {
	msg string
	err error
}

// updateRemoteKeys handles the Remote tab's bindings. It reports whether it
// consumed the key, so anything it ignores still reaches the shared tab/quit
// bindings.
func (m model) updateRemoteKeys(msg tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	// A pending rotation owns the keyboard: it is the one destructive action
	// here whose blast radius is every paired device.
	if m.confirmRotate {
		m.confirmRotate = false
		if msg.String() == "y" {
			m.remoteMsg, m.remoteErr = "", ""
			return true, m, m.rotateCmd()
		}
		m.remoteMsg = "Rotation cancelled."
		return true, m, nil
	}

	switch msg.String() {
	case "up", "k":
		if m.devCur > 0 {
			m.devCur--
		}
		return true, m, nil

	case "down", "j":
		if m.devCur < len(m.data.devices)-1 {
			m.devCur++
		}
		return true, m, nil

	case "d", "x", "delete":
		if m.devCur >= len(m.data.devices) {
			return true, m, nil
		}
		dev := m.data.devices[m.devCur]
		m.remoteMsg, m.remoteErr = "", ""
		return true, m, m.revokeDeviceCmd(dev)

	case "t":
		m.remoteMsg, m.remoteErr = "", ""
		return true, m, m.toggleRemoteCmd(!m.data.info.RemoteEnabled)

	case "R":
		m.confirmRotate = true
		return true, m, nil
	}
	return false, m, nil
}

func (m model) rotateCmd() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		code, err := c.rotateCode(ctx)
		if err != nil {
			return remoteActionMsg{err: err}
		}
		return remoteActionMsg{msg: "New code: " + code + " — every device must pair again with it."}
	}
}

func (m model) revokeDeviceCmd(dev devicePayload) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.revokeDevice(ctx, dev.ID); err != nil {
			return remoteActionMsg{err: err}
		}
		name := dev.DeviceName
		if name == "" {
			name = "device"
		}
		return remoteActionMsg{msg: "Revoked " + name + ". It must pair again to reconnect."}
	}
}

func (m model) toggleRemoteCmd(enabled bool) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.setRemoteEnabled(ctx, enabled); err != nil {
			return remoteActionMsg{err: err}
		}
		if enabled {
			return remoteActionMsg{msg: "Remote access on."}
		}
		return remoteActionMsg{msg: "Remote access off. This server is home-network only now."}
	}
}

func (m model) viewRemote() string {
	info := m.data.info
	var b strings.Builder

	if info.RemoteEnabled {
		fmt.Fprintf(&b, "Remote access : %s\n", valueStyle("on"))
		if info.ConnectionCode != "" {
			b.WriteString("Connection code\n")
			b.WriteString(codeStyle.Render(info.ConnectionCode) + "\n")
			b.WriteString(subtleStyle.Render("Anyone with this code can watch your library. Keep it private, and only share it with people you trust.") + "\n")
		}
	} else {
		fmt.Fprintf(&b, "Remote access : %s\n", warnStyle("off (home network only)"))
	}

	b.WriteString("\n" + titleStyle.Render("Paired devices") + "\n")
	if len(m.data.devices) == 0 {
		b.WriteString(subtleStyle.Render("None yet. Devices appear here when they pair.\n"))
	}
	for i, d := range m.data.devices {
		name := d.DeviceName
		if name == "" {
			name = "Unknown device"
		}
		line := fmt.Sprintf("%-30s %-12s last seen %s", trunc(name, 30), trunc(d.ProfileName, 12), agoRFC3339(d.LastSeenAt))
		marker := "  "
		if i == m.devCur {
			marker = lipgloss.NewStyle().Foreground(accent).Render("› ")
			line = lipgloss.NewStyle().Foreground(accent).Render(line)
		}
		b.WriteString(marker + line + "\n")
	}

	if m.confirmRotate {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(warn).Bold(true).
			Render("Rotate the connection code? Every paired device is signed out and must re-pair. y: yes • any other key: no") + "\n")
	}
	if m.remoteErr != "" {
		b.WriteString("\n" + warnStyle(m.remoteErr) + "\n")
	}
	if m.remoteMsg != "" {
		b.WriteString("\n" + subtleStyle.Render(m.remoteMsg) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// remoteHelp is the Remote tab's key legend.
func (m model) remoteHelp() string {
	if m.confirmRotate {
		return "y: rotate the code • any other key: cancel"
	}
	return "d: revoke device • t: remote on/off • R: rotate code • ↑/↓: select • tab: switch view • q: quit"
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// agoRFC3339 renders an RFC3339 timestamp as a rough "how long ago".
func agoRFC3339(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
