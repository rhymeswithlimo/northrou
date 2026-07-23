package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/api"
	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/rhymeswithlimo/northrou/backend/internal/db"
	"github.com/spf13/cobra"
)

// Device management and connection-code rotation from the terminal. Both
// prefer driving the running daemon's local API (which keeps one process
// owning the database and lets the daemon re-register with the coordinator on
// rotation); when nothing is running they fall back to the config file and
// database directly.

func newDevicesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "devices",
		Short: "List the devices paired with this server",
		Long: "Show every device currently paired with this server: its name, " +
			"which profile it watches as, and when it was last seen. Revoke one " +
			"with 'northrou devices revoke <id>'.",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessions, err := listDeviceSessions(cmd.Context())
			if err != nil {
				return err
			}
			if len(sessions) == 0 {
				notice("No devices are paired with this server.")
				return nil
			}
			fmt.Printf("%-10s %-34s %-14s %-12s %s\n", "ID", "DEVICE", "PROFILE", "LAST SEEN", "PAIRED")
			for _, s := range sessions {
				name := s.DeviceName
				if name == "" {
					name = "Unknown device"
				}
				fmt.Printf("%-10s %-34s %-14s %-12s %s\n",
					shorten(s.Key, 10), shorten(name, 34), shorten(s.ProfileName, 14),
					ago(s.LastUsedAt), s.CreatedAt.Local().Format("2006-01-02"))
			}
			notice("Revoke one with: northrou devices revoke <id>")
			return nil
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "revoke <id>",
		Short: "Sign a device out for good (it must re-pair with the connection code)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, err := resolveDeviceKey(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := revokeDeviceSession(cmd.Context(), key); err != nil {
				return err
			}
			notice("Device revoked. It is signed out as soon as its current session expires (minutes).")
			return nil
		},
	})
	return cmd
}

func newRotateCodeCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "rotate",
		Short: "Replace the connection code and sign every device out",
		Long: "Mint a fresh connection code and revoke every paired device's " +
			"session. Use this if the code has been shared further than you " +
			"wanted. Every device - including yours - must pair again with the " +
			"new code; devices on your home network reconnect on their own.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes && !confirm("Rotate the connection code? Every paired device is signed out and must re-pair. [y/N] ") {
				fmt.Println("Aborted.")
				return nil
			}
			code, err := rotateConnectionCode(cmd.Context())
			if err != nil {
				return err
			}
			notice("New connection code: %s\nShare it with your household; the old code no longer works.", code)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "rotate without confirmation")
	return cmd
}

// --- shared plumbing: API when the daemon runs, direct files/DB otherwise ---

// listDeviceSessions lists paired devices via the daemon when it is running,
// else straight from the database.
func listDeviceSessions(ctx context.Context) ([]db.DeviceSession, error) {
	cfg, err := loadConfigWithHint("devices")
	if err != nil {
		return nil, err
	}
	if alreadyServing(cfg.Server.Port) {
		c, err := newLocalAPI(ctx, cfg.Server.Port)
		if err != nil {
			return nil, err
		}
		var dtos []struct {
			ID          string    `json:"id"`
			DeviceName  string    `json:"device_name"`
			ProfileName string    `json:"profile_name"`
			PairedAt    time.Time `json:"paired_at"`
			LastSeenAt  time.Time `json:"last_seen_at"`
		}
		if err := c.get(ctx, "/api/admin/sessions", &dtos); err != nil {
			return nil, err
		}
		out := make([]db.DeviceSession, len(dtos))
		for i, d := range dtos {
			out[i] = db.DeviceSession{Key: d.ID, DeviceName: d.DeviceName,
				ProfileName: d.ProfileName, CreatedAt: d.PairedAt, LastUsedAt: d.LastSeenAt}
		}
		return out, nil
	}
	database, err := openDB(cfg)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	return database.ListDeviceSessions(ctx)
}

// resolveDeviceKey expands a shortened id (as printed by `devices`) to the
// full key, erroring when it is ambiguous.
func resolveDeviceKey(ctx context.Context, prefix string) (string, error) {
	sessions, err := listDeviceSessions(ctx)
	if err != nil {
		return "", err
	}
	var matches []string
	for _, s := range sessions {
		if strings.HasPrefix(s.Key, strings.TrimSuffix(prefix, "…")) {
			matches = append(matches, s.Key)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no paired device matches %q; see 'northrou devices'", prefix)
	default:
		return "", fmt.Errorf("%q matches %d devices; give more of the id", prefix, len(matches))
	}
}

func revokeDeviceSession(ctx context.Context, key string) error {
	cfg, err := loadConfigWithHint("devices")
	if err != nil {
		return err
	}
	if alreadyServing(cfg.Server.Port) {
		c, err := newLocalAPI(ctx, cfg.Server.Port)
		if err != nil {
			return err
		}
		return c.del(ctx, "/api/admin/sessions/"+key)
	}
	database, err := openDB(cfg)
	if err != nil {
		return err
	}
	defer database.Close()
	return database.RevokeDeviceSession(ctx, key)
}

// rotateConnectionCode mints and applies a fresh code. Through the daemon when
// running (which also re-registers with the coordinator); on files otherwise.
func rotateConnectionCode(ctx context.Context) (string, error) {
	cfg, err := loadConfigWithHint("cc rotate")
	if err != nil {
		return "", err
	}
	if alreadyServing(cfg.Server.Port) {
		c, err := newLocalAPI(ctx, cfg.Server.Port)
		if err != nil {
			return "", err
		}
		var out struct {
			ConnectionCode string `json:"connection_code"`
		}
		if err := c.post(ctx, "/api/admin/connection-code/rotate", &out); err != nil {
			return "", err
		}
		return out.ConnectionCode, nil
	}

	// Daemon stopped: rewrite config and revoke sessions directly. The peer
	// registers under the new code whenever the server next starts.
	cfg.Remote.ConnectionCode = api.NewConnectionCode()
	if err := cfg.Save(flagConfigPath); err != nil {
		return "", err
	}
	database, err := openDB(cfg)
	if err != nil {
		return "", fmt.Errorf("code saved, but revoking sessions failed: %w", err)
	}
	defer database.Close()
	if err := database.RevokeAllTokens(ctx); err != nil {
		return "", fmt.Errorf("code saved, but revoking sessions failed: %w", err)
	}
	return cfg.Remote.ConnectionCode, nil
}

func loadConfigWithHint(cmdName string) (*config.Config, error) {
	cfg, err := config.Load(flagConfigPath)
	if err != nil {
		return nil, fmt.Errorf("couldn't read config at %s: %w\n"+
			"(if Northrou runs as a system service, try: sudo northrou %s)",
			flagConfigPath, err, cmdName)
	}
	return cfg, nil
}

func openDB(cfg *config.Config) (*db.DB, error) {
	return db.Open(filepath.Join(cfg.Server.DataDir, "northrou.db"))
}

func confirm(prompt string) bool {
	fmt.Print(prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.EqualFold(strings.TrimSpace(line), "y")
}

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func ago(t time.Time) string {
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
