package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/rhymeswithlimo/northrou/backend/internal/app"
	"github.com/rhymeswithlimo/northrou/backend/internal/service"
	"github.com/rhymeswithlimo/northrou/backend/internal/setup"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the Northrou server (daemon)",
		Long: "Run the Northrou media server. This is the command the installed " +
			"system service invokes. It loads config, opens the database, and " +
			"serves the HTTP API until interrupted. On first run it opens the " +
			"setup wizard in your browser.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// When launched by the OS service manager (not a terminal), hand
			// control to kardianos so Start/Stop are dispatched correctly.
			if !service.Interactive() {
				return service.RunManaged(flagConfigPath)
			}

			a, err := app.New(flagConfigPath)
			if err != nil {
				return err
			}
			defer a.Close()

			ctx, stop := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer stop()

			if a.FirstRun && !noBrowser {
				url := fmt.Sprintf("http://localhost:%d/", a.Cfg.Server.Port)
				slog.Warn("opening setup wizard on first run", "url", url)
				setup.OpenBrowser(url)
			}

			return a.Run(ctx)
		},
	}
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "do not open the setup wizard in a browser on first run")
	return cmd
}
