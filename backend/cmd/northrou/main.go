// Command northrou is the single self-contained Northrou server binary. It runs
// the daemon, manages the OS service, drives first-run setup, opens the admin
// TUI, self-updates, and triggers library scans, all as subcommands.
package main

import (
	"os"

	"github.com/rhymeswithlimo/northrou/backend/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
