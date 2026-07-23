package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/app"
	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/rhymeswithlimo/northrou/backend/internal/setup"
	"github.com/rhymeswithlimo/northrou/backend/internal/tui"
	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Set up this server in your terminal (name, media folders, remote access)",
		Long: "Walk through first-run setup right here in the terminal: name your " +
			"server, point it at your media folders, optionally add a TMDB key for " +
			"artwork and descriptions, and choose whether it is reachable away from " +
			"home. Finishing setup shows the connection code your devices use to " +
			"pair.\n\n" +
			"If Northrou is already running (for example as an installed system " +
			"service), setup talks to that instance; otherwise it starts the server " +
			"itself for the duration of this command. Re-running setup on a " +
			"configured server shows its name, connection code, and addresses.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := config.LoadOrInit(flagConfigPath)
			if err != nil {
				return err
			}
			port := cfg.Server.Port
			base := fmt.Sprintf("http://localhost:%d", port)

			// Northrou may already be running as an installed system service
			// (which starts immediately on `northrou install`, run by the
			// install script). Then there is nothing to start here - the wizard
			// drives the running instance. Otherwise run the server ourselves,
			// in-process, for as long as the wizard is open.
			var a *app.App
			if !alreadyServing(port) {
				a, err = app.New(flagConfigPath)
				if err != nil {
					return err
				}
				defer a.Close()

				// Start the server first: the wizard is a client of the local
				// API. If the port is held by another program, portConflict
				// turns the raw bind error into actionable guidance.
				if err := a.Server.Start(); err != nil {
					return portConflict(err, port)
				}
				// The TUI owns the terminal from here: a daemon log line
				// printed to stderr would draw right across it. Drop the
				// terminal handler before StartBackground tees in the log
				// file, so logging continues on disk only.
				slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
				ctx, cancel := context.WithCancel(cmd.Context())
				defer cancel()
				a.StartBackground(ctx)
			}

			if err := tui.RunSetup(base, flagConfigPath); err != nil {
				return err
			}
			printSetupOutro(a != nil)
			return nil
		},
	}
}

// printSetupOutro leaves the operator with what still matters after the TUI's
// alt-screen is gone: whether the server keeps running, and how to get back in.
func printSetupOutro(ranInProcess bool) {
	var b strings.Builder
	if ranInProcess {
		fmt.Fprintln(&b, "Northrou stops when this command exits.")
		fmt.Fprintln(&b, "  Keep it running in the background:  sudo northrou install")
		fmt.Fprintln(&b, "  Or run it in the foreground:        northrou serve")
	} else {
		fmt.Fprintln(&b, "Northrou keeps running as a system service.")
	}
	fmt.Fprint(&b, "Dashboard: northrou admin • Connection code: northrou cc • Status: northrou status")
	notice("%s", b.String())
}

// printSetupURLs prints every address the server is reachable at, straight to
// stdout (not a log line, which can be filtered or missed). On a headless box
// "localhost" refers to the box itself, not whatever device you are reading
// this from, so this also lists the machine's LAN addresses -
// headless/self-hosted is the primary use case here, not an edge case.
func printSetupURLs(port int) {
	var b strings.Builder
	fmt.Fprintln(&b, "Open one of these in a browser:")
	fmt.Fprintf(&b, "  http://localhost:%d/   (if you are on this machine)", port)
	for _, ip := range setup.LocalIPv4s() {
		fmt.Fprintf(&b, "\n  http://%s:%d/   (from another device on your network)", ip, port)
	}
	notice("%s", b.String())
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
