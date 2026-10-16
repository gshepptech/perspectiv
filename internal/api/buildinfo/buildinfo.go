// Package buildinfo carries the version, commit and build date stamped into
// the binary by the Makefile's -ldflags -X flags.
package buildinfo

import "runtime"

// Stamped at build time with -ldflags -X perspectiv/internal/api/buildinfo.<Name>=<value>.
// The defaults are what a plain `go run` or `go test` reports.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// Info is the build information the /about endpoint serves.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"go_version"`
}

// Get returns the stamped build information.
func Get() Info {
	return Info{Version: Version, Commit: Commit, Date: Date, GoVersion: runtime.Version()}
}
