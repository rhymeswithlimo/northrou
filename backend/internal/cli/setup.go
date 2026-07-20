package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
				announceAlreadyRunning(a.Cfg.Server.Port)
				return nil
			}

			ctx, stop := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer stop()

			// Start server first, then open the browser once it is listening.
			// If the port is held by another program, portConflict turns the
			// raw bind error into actionable guidance.
			if err := a.Server.Start(); err != nil {
				return portConflict(err, a.Cfg.Server.Port)
			}
			printSetupURLs(a.Cfg.Server.Port)
			setup.OpenBrowser(url)
			<-ctx.Done()
			return a.Close()
		},
	}
}

// printSetupURLs prints every address the setup wizard is reachable at,
// straight to stdout (not a log line, which can be filtered or missed). On a
// headless box "localhost" refers to the box itself, not whatever device you
// are reading this from, so this also lists the machine's LAN addresses -
// headless/self-hosted is the primary use case here, not an edge case.
func printSetupURLs(port int) {
	var b strings.Builder
	fmt.Fprintln(&b, "Setup wizard ready. Open one of these in a browser:")
	fmt.Fprintf(&b, "  http://localhost:%d/   (if you are on this machine)", port)
	for _, ip := range localIPv4s() {
		fmt.Fprintf(&b, "\n  http://%s:%d/   (from another device on your network)", ip, port)
	}
	notice("%s", b.String())
}

// localIPv4s lists this machine's non-loopback IPv4 addresses.
func localIPv4s() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var ips []string
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() {
			continue
		}
		if v4 := ipNet.IP.To4(); v4 != nil {
			ips = append(ips, v4.String())
		}
	}
	return ips
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
