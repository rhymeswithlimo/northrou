// Package setup handles Northrou's first-run experience: opening the setup
// wizard in the user's browser and serving its static assets. The actual
// account/media configuration is performed through the /api/setup endpoints.
package setup

import (
	"log/slog"
	"os/exec"
	"runtime"
)

// OpenBrowser attempts to open url in the default browser. Failures are logged,
// not fatal, so the user can navigate manually.
func OpenBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default: // linux, *bsd
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		slog.Warn("could not open browser automatically", "url", url, "err", err)
		return
	}
	slog.Info("opened setup wizard in browser", "url", url)
}
