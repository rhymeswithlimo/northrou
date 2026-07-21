package cli

import (
	"fmt"

	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/spf13/cobra"
)

// newConnectionCodeCmd prints this server's remote connection code - what apps
// and the web client enter to pair with the box. A convenience so an operator
// doesn't have to dig it out of config.toml or the admin TUI.
func newConnectionCodeCmd() *cobra.Command {
	return &cobra.Command{
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
			if cfg.Remote.ConnectionCode == "" {
				return fmt.Errorf("no connection code yet - finish setup first with 'northrou setup'")
			}
			notice("%s", cfg.Remote.ConnectionCode)
			return nil
		},
	}
}
