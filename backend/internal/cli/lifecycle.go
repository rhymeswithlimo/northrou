package cli

import (
	"errors"

	"github.com/rhymeswithlimo/northrou/backend/internal/service"
	"github.com/spf13/cobra"
)

// errNotInstalled is what start/stop/restart report when there is no service
// to control, with the two ways forward.
var errNotInstalled = errors.New("Northrou is not installed as a system service.\n" +
	"Install it with:  sudo northrou install\n" +
	"Or run it in the foreground with:  northrou serve")

// start / stop / restart control the installed system service. Before these
// existed the only way to bounce the server was `uninstall && install`, which
// is a rough thing to ask of someone who just wants a restart.

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the Northrou system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := service.Start(flagConfigPath); err != nil {
				return needsRoot(cmd, notInstalledHint(err))
			}
			notice("Northrou started.")
			return nil
		},
	}
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the Northrou system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := service.Stop(flagConfigPath); err != nil {
				return needsRoot(cmd, notInstalledHint(err))
			}
			notice("Northrou stopped. Start it again with 'northrou start'.")
			return nil
		},
	}
}

func newRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the Northrou system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := service.Restart(flagConfigPath); err != nil {
				return needsRoot(cmd, notInstalledHint(err))
			}
			notice("Northrou restarted.")
			return nil
		},
	}
}

// notInstalledHint rewrites the service manager's "not installed" error into
// the fix, and passes everything else through for needsRoot to inspect.
func notInstalledHint(err error) error {
	st, stErr := service.GetStatus(flagConfigPath)
	if stErr == nil && st == service.StatusNotInstalled {
		return errNotInstalled
	}
	return err
}
