package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewSnapshotsCommand_MissingConfigFile(t *testing.T) {
	cmd := NewSnapshotsCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--config", filepath.Join(t.TempDir(), "missing.yaml")})

	err := cmd.Execute()
	if got := ExitCode(err); got != 2 {
		t.Fatalf("ExitCode(Execute()) = %d, want 2 (stderr: %s)", got, errOut.String())
	}
}

func TestNewSnapshotsCommand_InvalidConfig(t *testing.T) {
	configPath := writeTestConfig(t, `
retention:
  keep_daily: -1
`)

	cmd := NewSnapshotsCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--config", configPath})

	err := cmd.Execute()
	if got := ExitCode(err); got != 2 {
		t.Fatalf("ExitCode(Execute()) = %d, want 2 (stderr: %s)", got, errOut.String())
	}
	if !strings.Contains(errOut.String(), "invalid configuration") {
		t.Errorf("stderr = %q, want it to mention invalid configuration", errOut.String())
	}
}
