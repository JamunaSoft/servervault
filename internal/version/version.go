// Package version holds ServerVault's build metadata. Version, Commit, and
// Date are set at link time via -ldflags (see Makefile and
// .github/workflows/release.yml); they default to placeholder values for
// unversioned "go build"/"go run" invocations.
package version

import "runtime"

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Info is a snapshot of build metadata plus the runtime environment the
// binary is currently executing under.
type Info struct {
	Version   string
	Commit    string
	Date      string
	GoVersion string
	OS        string
	Arch      string
}

// Get returns the current build's Info, combining the link-time variables
// with the runtime Go version and platform.
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}
