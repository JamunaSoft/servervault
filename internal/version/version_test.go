package version

import (
	"runtime"
	"testing"
)

func TestGet(t *testing.T) {
	origVersion, origCommit, origDate := Version, Commit, Date
	t.Cleanup(func() {
		Version, Commit, Date = origVersion, origCommit, origDate
	})

	tests := []struct {
		name    string
		version string
		commit  string
		date    string
	}{
		{name: "defaults", version: "dev", commit: "none", date: "unknown"},
		{name: "ldflags-set", version: "v1.2.3", commit: "abc1234", date: "2026-07-11T00:00:00Z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Version, Commit, Date = tt.version, tt.commit, tt.date

			info := Get()

			if info.Version != tt.version {
				t.Errorf("Version = %q, want %q", info.Version, tt.version)
			}
			if info.Commit != tt.commit {
				t.Errorf("Commit = %q, want %q", info.Commit, tt.commit)
			}
			if info.Date != tt.date {
				t.Errorf("Date = %q, want %q", info.Date, tt.date)
			}
			if info.GoVersion != runtime.Version() {
				t.Errorf("GoVersion = %q, want %q", info.GoVersion, runtime.Version())
			}
			if info.OS != runtime.GOOS {
				t.Errorf("OS = %q, want %q", info.OS, runtime.GOOS)
			}
			if info.Arch != runtime.GOARCH {
				t.Errorf("Arch = %q, want %q", info.Arch, runtime.GOARCH)
			}
		})
	}
}
