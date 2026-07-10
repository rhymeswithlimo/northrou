package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rhymeswithlimo/northrou/backend/internal/app"
	"github.com/rhymeswithlimo/northrou/backend/internal/setup"
	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Run the setup wizard (starts the server and opens a browser)",
		Long: "Start Northrou and open the setup wizard in your browser so you " +
			"can create an account, point at your media folders, and get a " +
			"remote connection code. Runs until interrupted.",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := app.New(flagConfigPath)
			if err != nil {
				return err
			}
			defer a.Close()

			ctx, stop := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer stop()

			url := fmt.Sprintf("http://localhost:%d/", a.Cfg.Server.Port)
			// Start server first, then open the browser once it is listening.
			if err := a.Server.Start(); err != nil {
				return err
			}
			setup.OpenBrowser(url)
			<-ctx.Done()
			return a.Close()
		},
	}
}
