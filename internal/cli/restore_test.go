package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewRestoreCommand_RequiresSnapshot(t *testing.T) {
	cmd := NewRestoreCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--target", "files"})

	err := cmd.Execute()
	if got := ExitCode(err); got != 2 {
		t.Fatalf("ExitCode(Execute()) = %d, want 2 (stderr: %s)", got, errOut.String())
	}
	if !strings.Contains(errOut.String(), "--snapshot is required") {
		t.Errorf("stderr = %q, want it to mention --snapshot is required", errOut.String())
	}
}

func TestNewRestoreCommand_RequiresValidTarget(t *testing.T) {
	tests := []string{"", "bogus", "FILES", "temp-db "}
	for _, target := range tests {
		t.Run(target, func(t *testing.T) {
			cmd := NewRestoreCommand()
			var out, errOut bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&errOut)
			cmd.SetArgs([]string{"--snapshot", "abc123", "--target", target})

			err := cmd.Execute()
			if got := ExitCode(err); got != 2 {
				t.Fatalf("ExitCode(Execute()) = %d, want 2 (stderr: %s)", got, errOut.String())
			}
		})
	}
}

func TestNewRestoreCommand_RequiresValidOutput(t *testing.T) {
	cmd := NewRestoreCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--snapshot", "abc123", "--target", "files", "--output", "yaml"})

	err := cmd.Execute()
	if got := ExitCode(err); got != 2 {
		t.Fatalf("ExitCode(Execute()) = %d, want 2 (stderr: %s)", got, errOut.String())
	}
	if !strings.Contains(errOut.String(), "--output") {
		t.Errorf("stderr = %q, want it to mention --output", errOut.String())
	}
}

func TestNewRestoreCommand_MissingConfigFile(t *testing.T) {
	cmd := NewRestoreCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{
		"--snapshot", "abc123", "--target", "files",
		"--config", filepath.Join(t.TempDir(), "missing.yaml"),
	})

	err := cmd.Execute()
	if got := ExitCode(err); got != 2 {
		t.Fatalf("ExitCode(Execute()) = %d, want 2 (stderr: %s)", got, errOut.String())
	}
}

func TestNewRestoreCommand_InvalidConfig(t *testing.T) {
	configPath := writeTestConfig(t, `
retention:
  keep_daily: -1
`)

	cmd := NewRestoreCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--snapshot", "abc123", "--target", "files", "--config", configPath})

	err := cmd.Execute()
	if got := ExitCode(err); got != 2 {
		t.Fatalf("ExitCode(Execute()) = %d, want 2 (stderr: %s)", got, errOut.String())
	}
	if !strings.Contains(errOut.String(), "invalid configuration") {
		t.Errorf("stderr = %q, want it to mention invalid configuration", errOut.String())
	}
}

// TestNewRestoreCommand_FlagValidationHappensBeforeAnyRealWork proves
// --target/--output/--snapshot are validated before config is even
// loaded (bad-target and bad-output tests above use no --config flag at
// all and still exit 2 for the flag reason, not a missing-config
// reason) -- exercised implicitly by the tests above via distinct
// stderr assertions; this test only pins the ordering explicitly for a
// combination of two simultaneous problems, so a future refactor can't
// silently swap which one wins.
func TestNewRestoreCommand_FlagValidationHappensBeforeAnyRealWork(t *testing.T) {
	cmd := NewRestoreCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	// Bad --target AND a nonexistent config file: the flag error must
	// win, since it never needs to touch the filesystem.
	cmd.SetArgs([]string{
		"--snapshot", "abc123", "--target", "bogus",
		"--config", filepath.Join(t.TempDir(), "missing.yaml"),
	})

	err := cmd.Execute()
	if got := ExitCode(err); got != 2 {
		t.Fatalf("ExitCode(Execute()) = %d, want 2", got)
	}
	if !strings.Contains(errOut.String(), "--target") {
		t.Errorf("stderr = %q, want the --target error to be reported first", errOut.String())
	}
}
