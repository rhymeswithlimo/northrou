package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
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

			port := a.Cfg.Server.Port

			// The installed service almost certainly already holds this port.
			// Rather than fail to bind with a raw "address already in use",
			// detect the running Northrou and point at its URL, exactly as
			// `setup` does. This is the common case: someone runs `serve` by
			// hand while the service is up.
			if alreadyServing(port) {
				announceAlreadyRunning(port)
				return nil
			}

			ctx, stop := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer stop()

			if a.FirstRun && !noBrowser {
				url := fmt.Sprintf("http://localhost:%d/", port)
				slog.Warn("opening setup wizard on first run", "url", url)
				setup.OpenBrowser(url)
			}

			return portConflict(a.Run(ctx), port)
		},
	}
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "do not open the setup wizard in a browser on first run")
	return cmd
}

// announceAlreadyRunning tells the user Northrou is already up on this port and
// prints the URL(s) to open, instead of leaving them staring at a bind error.
func announceAlreadyRunning(port int) {
	notice("Northrou is already running as a service.")
	printSetupURLs(port)
}

// portConflict translates a server-start failure into actionable guidance when
// the port is already taken. It returns:
//   - nil if err is nil, or if the port is held by an already-running Northrou
//     (it prints that instance's URL and the caller should just exit 0);
//   - a clear, fix-it message when another program holds the port;
//   - err unchanged for anything that is not a port conflict.
func portConflict(err error, port int) error {
	if err == nil || !errors.Is(err, syscall.EADDRINUSE) {
		return err
	}
	// A race: the service came up between our pre-check and our own bind, or a
	// second Northrou is up. Either way, it's ours - point at it.
	if alreadyServing(port) {
		announceAlreadyRunning(port)
		return nil
	}
	// Something else owns the port. Tell the user how to move Northrou off it.
	hint := ""
	if free := firstFreePort(port + 1); free != 0 {
		hint = fmt.Sprintf(" %d is free.", free)
	}
	return fmt.Errorf("port %d is in use by another program, so Northrou can't start.\n"+
		"Give Northrou a different port: in %s, set  port = <number>  under the [server] "+
		"section, then restart the service.%s", port, flagConfigPath, hint)
}

// firstFreePort returns the first bindable TCP port at or after `from` (scanning
// a small range), or 0 if none is free, for suggesting an alternative.
func firstFreePort(from int) int {
	for p := from; p <= from+20 && p <= 65535; p++ {
		ln, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(p)))
		if err == nil {
			_ = ln.Close()
			return p
		}
	}
	return 0
}
