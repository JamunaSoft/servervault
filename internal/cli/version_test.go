package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/JamunaSoft/servervault/internal/version"
)

func TestNewVersionCommand(t *testing.T) {
	origVersion, origCommit, origDate := version.Version, version.Commit, version.Date
	t.Cleanup(func() { version.Version, version.Commit, version.Date = origVersion, origCommit, origDate })
	version.Version, version.Commit, version.Date = "v1.2.3", "abc1234", "2026-07-11"

	cmd := NewVersionCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(): unexpected error: %v", err)
	}

	got := out.String()
	for _, want := range []string{"ServerVault", "v1.2.3", "abc1234", "2026-07-11"} {
		if !strings.Contains(got, want) {
			t.Errorf("version output = %q, want it to contain %q", got, want)
		}
	}
}
