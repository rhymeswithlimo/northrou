// Package cli defines Northrou's command-line interface: a single multi-command
// binary that runs the server, manages the OS service, launches setup, opens
// the admin TUI, self-updates, and triggers scans.
package cli

import (
	"errors"
	"fmt"
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
		newMatchCmd(),
		newUpdateCmd(),
		newConnectionCodeCmd(),
	)
	return root
}

// Execute runs the CLI and returns the process exit code. A failing command
// prints its error plainly to stderr rather than as a structured slog line:
// these are interactive commands, and "Error: <message>" is what a user
// expects, not "level=ERROR msg=\"command failed\" err=..." (which would also
// mangle multi-line guidance like needsRoot's into literal \n).
func Execute() int {
	err := NewRootCmd().Execute()
	if err == nil {
		return 0
	}
	// needsRoot's sudo/Administrator guidance is operator-facing instruction,
	// not part of the failure - print it highlighted, same as notice(), so it
	// reads as "here's what to do" rather than more error text.
	if hintErr, ok := errors.AsType[*rootHintError](err); ok {
		fmt.Fprintln(os.Stderr, "Error:", hintErr.err)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, highlightErr(hintErr.hint))
		return 1
	}
	fmt.Fprintln(os.Stderr, "Error:", err)
	return 1
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
