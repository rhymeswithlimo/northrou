// Package cli defines Northrou's command-line interface: a single multi-command
// binary that runs the server, manages the OS service, launches setup, opens
// the admin TUI, self-updates, and triggers scans.
package cli

import (
	"log/slog"
	"os"

	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/spf13/cobra"
)

// persistent flags shared by all subcommands.
var (
	flagConfigPath string
	flagVerbose    bool
)

// NewRootCmd builds the root cobra command with all subcommands attached.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "northrou",
		Short:         "Northrou, a self-hosted media server for your physical collection",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			setupLogger(flagVerbose)
		},
	}

	root.PersistentFlags().StringVar(&flagConfigPath, "config", config.ConfigPath(),
		"path to config.toml")
	root.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false,
		"enable debug logging")

	root.AddCommand(
		newVersionCmd(),
		newServeCmd(),
		newSetupCmd(),
		newInstallCmd(),
		newUninstallCmd(),
		newAdminCmd(),
		newScanCmd(),
		newUpdateCmd(),
	)
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	if err := NewRootCmd().Execute(); err != nil {
		slog.Error("command failed", "err", err)
		return 1
	}
	return 0
}

// setupLogger configures the global slog logger.
func setupLogger(verbose bool) {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h))
}
