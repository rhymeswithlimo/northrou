package cli

import (
	"context"
	"fmt"

	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/spf13/cobra"
)

// newConnectionCodeCmd prints this server's remote connection code - what apps
// and the web client enter to pair with the box. A convenience so an operator
// doesn't have to dig it out of config.toml or the admin TUI. `cc rotate`
// replaces it.
func newConnectionCodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "cc",
		Aliases: []string{"code", "connection-code"},
		Short:   "Print this server's connection code (for pairing apps)",
		Long: "Print the connection code that apps and the web client use to " +
			"reach this server from anywhere - the same code the setup wizard " +
			"showed and Server admin lists.\n\n" +
			"It reads the config file this command can see, so on a box where " +
			"Northrou runs as a system service (as root), run it with sudo.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(flagConfigPath)
			if err != nil {
				return fmt.Errorf("couldn't read config at %s: %w\n"+
					"(if Northrou runs as a system service, try: sudo northrou cc)",
					flagConfigPath, err)
			}
			// Prefer the running daemon's live code over the config file: `cc
			// rotate` and the admin TUI drive the daemon, and the daemon owns the
			// config.toml it actually registers with the coordinator - which may
			// not be the file this command reads (a root service vs. a user
			// shell). Reading disk here would print a stale code the box no
			// longer answers to. Fall back to the file when nothing is serving.
			if code := runningServerCode(cmd.Context(), cfg.Server.Port); code != "" {
				notice("%s", code)
				return nil
			}
			if cfg.Remote.ConnectionCode == "" {
				return fmt.Errorf("no connection code yet - finish setup first with 'northrou setup'")
			}
			notice("%s", cfg.Remote.ConnectionCode)
			return nil
		},
	}
	cmd.AddCommand(newRotateCodeCmd())
	return cmd
}

// runningServerCode returns the connection code the daemon on this port is
// actually registered with, or "" if nothing is serving or it can't be read.
// The request is local, so /api/admin/config includes the code (a secret it
// withholds from remote sessions). Best-effort: any failure falls back to the
// config file.
func runningServerCode(ctx context.Context, port int) string {
	if !alreadyServing(port) {
		return ""
	}
	c, err := newLocalAPI(ctx, port)
	if err != nil {
		return ""
	}
	var out struct {
		ConnectionCode string `json:"connection_code"`
	}
	if err := c.get(ctx, "/api/admin/config", &out); err != nil {
		return ""
	}
	return out.ConnectionCode
}
