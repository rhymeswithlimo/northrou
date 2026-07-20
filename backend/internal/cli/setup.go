package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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

			url := fmt.Sprintf("http://localhost:%d/", a.Cfg.Server.Port)

			// Northrou may already be running as an installed system service
			// (which starts immediately on `northrou install`, run by the
			// install script). In that case there is nothing to start here -
			// the running instance already serves this same wizard, and
			// trying to bind the same port again would just fail.
			if alreadyServing(a.Cfg.Server.Port) {
				fmt.Printf("Northrou is already running as a service.\nOpen %s in your browser to finish setup.\n", url)
				return nil
			}

			ctx, stop := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer stop()

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

// alreadyServing reports whether something is already answering health
// checks on the given port, i.e. an installed system service is already
// running this same binary.
func alreadyServing(port int) bool {
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/api/health", port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
