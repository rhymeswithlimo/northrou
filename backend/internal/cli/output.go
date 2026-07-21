package cli

import (
	"fmt"
	"os"
)

// Northrou's own operator-facing CLI messages (setup URLs, service and update
// status) are printed through notice() so they stand out from interleaved log
// lines and third-party output instead of blending in.

const (
	ansiReset = "\033[0m"
	ansiBold  = "\033[1m"
	ansiCyan  = "\033[36m"
)

// useColor is resolved once: highlight only when stdout is a real terminal and
// NO_COLOR is unset. Piped output and the journald/systemd logs the service
// writes to must stay plain, or the escape codes turn into garbage there.
var useColor = wantColor(os.Stdout, os.LookupEnv)

// useColorErr is useColor's counterpart for stderr, where Execute prints
// command failures: stdout and stderr can be redirected independently (e.g.
// `northrou update 2>err.log`), so each stream decides for itself.
var useColorErr = wantColor(os.Stderr, os.LookupEnv)

// wantColor is the testable core: it takes the output file and an env lookup so
// the TTY and NO_COLOR branches can be exercised without a real terminal.
func wantColor(out *os.File, lookupEnv func(string) (string, bool)) bool {
	if _, ok := lookupEnv("NO_COLOR"); ok {
		return false
	}
	fi, err := out.Stat()
	if err != nil {
		return false
	}
	// A character device is a terminal; a pipe or regular file is not. This is
	// the pure-Go isatty check, so it adds no dependency (see CLAUDE.md: never
	// introduce a CGo dependency).
	return fi.Mode()&os.ModeCharDevice != 0
}

// notice prints an operator-facing Northrou message, highlighted so it stands
// out on a terminal and left plain everywhere else. A colour+bold sequence
// persists across newlines until the reset, so a multi-line message (the setup
// URL block) is highlighted as one unit with a single wrap.
func notice(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if useColor {
		msg = ansiBold + ansiCyan + msg + ansiReset
	}
	fmt.Println(msg)
}

// highlightErr applies notice's highlight to text destined for stderr, e.g.
// the actionable part of an error (see rootHintError in privilege.go), so
// operator guidance stands out there the same way it does on stdout.
func highlightErr(s string) string {
	if !useColorErr {
		return s
	}
	return ansiBold + ansiCyan + s + ansiReset
}
