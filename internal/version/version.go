// Package version holds build information stamped at link time via
// -ldflags -X (see the Makefile build targets). Defaults apply to unstamped
// builds such as go run and go test.
package version

var (
	// Version is the release version, e.g. from `git describe --tags --always --dirty`.
	Version = "dev"
	// Commit is the short git commit hash, e.g. from `git rev-parse --short HEAD`.
	Commit = "none"
	// BuiltAt is the UTC build timestamp, e.g. 2026-09-06T12:00:00Z.
	BuiltAt = "unknown"
)
