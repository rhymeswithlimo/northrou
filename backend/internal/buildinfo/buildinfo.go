// Package buildinfo exposes version metadata stamped in at build time via
// -ldflags, with sensible fallbacks for `go run` / `go build` development.
package buildinfo

import "runtime"

var (
	// Version is the semantic version, set with
	// -ldflags "-X .../buildinfo.Version=v1.2.3".
	Version = "dev"
	// Commit is the short git SHA.
	Commit = "none"
	// Date is the build timestamp (RFC3339).
	Date = "unknown"
)

// Platform returns the GOOS/GOARCH pair this binary was built for.
func Platform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

// String returns a human-readable one-line version summary.
func String() string {
	return "northrou " + Version + " (" + Commit + ", " + Date + ", " + Platform() + ")"
}
