package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"

	"github.com/spf13/cobra"
)

// needsRoot wraps err with actionable guidance when a privileged command fails
// on a permission error that re-running with elevated privileges would fix.
// Replacing the installed binary (`update`) or registering a system service
// (`install`/`uninstall`) writes to root-owned locations (/usr/local/bin,
// systemd/launchd unit dirs); run as a normal user they fail with a bare
// "permission denied". This turns that into a message telling the user exactly
// how to re-run, instead of surfacing the raw syscall error.
//
// It is deliberately reactive (wrapping a real error) rather than a proactive
// "am I root?" pre-check: a non-root install into ~/.local/bin updates itself
// just fine without sudo, and a proactive check would wrongly nag those users.
func needsRoot(cmd *cobra.Command, err error) error {
	return elevationHint(err, cmd.CommandPath(), os.Geteuid(), runtime.GOOS)
}

// elevationHint is the pure core of needsRoot, taking euid and goos as
// parameters so the Windows and non-root paths are testable on any host.
// err is returned unchanged when it is nil, is not a permission error, or we
// are already root (euid 0) - in which case elevation is not the problem and
// telling the user to sudo would be wrong. os.Geteuid returns -1 on Windows,
// so the euid==0 guard never trips there and the hint still fires.
func elevationHint(err error, commandPath string, euid int, goos string) error {
	if err == nil || !errors.Is(err, fs.ErrPermission) || euid == 0 {
		return err
	}
	if goos == "windows" {
		return fmt.Errorf("%w\n\nThis needs administrator privileges. Re-run %q "+
			"from a terminal opened as Administrator.", err, commandPath)
	}
	return fmt.Errorf("%w\n\nThis needs root. Re-run it with sudo:\n  sudo %s",
		err, commandPath)
}
