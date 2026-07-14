package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewPruneCommand_RequiresValidOutput(t *testing.T) {
	cmd := NewPruneCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--output", "yaml"})

	err := cmd.Execute()
	if got := ExitCode(err); got != 2 {
		t.Fatalf("ExitCode(Execute()) = %d, want 2 (stderr: %s)", got, errOut.String())
	}
	if !strings.Contains(errOut.String(), "--output") {
		t.Errorf("stderr = %q, want it to mention --output", errOut.String())
	}
}

func TestNewPruneCommand_MissingConfigFile(t *testing.T) {
	cmd := NewPruneCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--config", filepath.Join(t.TempDir(), "missing.yaml")})

	err := cmd.Execute()
	if got := ExitCode(err); got != 2 {
		t.Fatalf("ExitCode(Execute()) = %d, want 2 (stderr: %s)", got, errOut.String())
	}
}

func TestNewPruneCommand_InvalidConfig(t *testing.T) {
	configPath := writeTestConfig(t, `
retention:
  keep_daily: 7
  max_delete_count: 0
`)

	cmd := NewPruneCommand()
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
	if !strings.Contains(errOut.String(), "retention.max_delete_count") {
		t.Errorf("stderr = %q, want it to mention retention.max_delete_count", errOut.String())
	}
}

// TestNewPruneCommand_FlagValidationHappensBeforeConfig proves --output
// is validated before config is even loaded -- mirrors
// TestNewRestoreCommand_FlagValidationHappensBeforeAnyRealWork.
func TestNewPruneCommand_FlagValidationHappensBeforeConfig(t *testing.T) {
	cmd := NewPruneCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{
		"--output", "bogus",
		"--config", filepath.Join(t.TempDir(), "missing.yaml"),
	})

	err := cmd.Execute()
	if got := ExitCode(err); got != 2 {
		t.Fatalf("ExitCode(Execute()) = %d, want 2", got)
	}
	if !strings.Contains(errOut.String(), "--output") {
		t.Errorf("stderr = %q, want the --output error to be reported first", errOut.String())
	}
}

func TestNewPruneCommand_ValidConfig_FailsAtResticInvocation(t *testing.T) {
	// A structurally valid config that Plan() cannot get past without a
	// real restic binary/repository -- proves the CLI wiring (config
	// load, validation, Planner construction, Plan invocation) is
	// correct up to that boundary, the same way
	// TestNewRestoreCommand_* stops at the point where restic would
	// need to actually run. Exit code 1, not 2: this is Plan failing at
	// runtime, not a config/usage error.
	dir := t.TempDir()
	configPath := writeTestConfig(t, `
restic:
  repository: "local:`+filepath.Join(dir, "repo")+`"
  password_file: "/nonexistent/restic-password"
backup:
  paths:
    - "`+filepath.Join(dir, "payload")+`"
  lock_file: "`+filepath.Join(dir, "backup.lock")+`"
restore:
  staging_root: "`+filepath.Join(dir, "restore")+`"
  lock_file: "`+filepath.Join(dir, "restore.lock")+`"
retention:
  keep_daily: 7
  lock_file: "`+filepath.Join(dir, "prune.lock")+`"
postgres:
  enabled: false
`)

	cmd := NewPruneCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--config", configPath, "--dry-run"})

	err := cmd.Execute()
	if got := ExitCode(err); got != 1 {
		t.Fatalf("ExitCode(Execute()) = %d, want 1 (stderr: %s)", got, errOut.String())
	}
	if !strings.Contains(errOut.String(), "prune: plan:") {
		t.Errorf("stderr = %q, want it to identify the plan step", errOut.String())
	}
}

func TestNewPruneCommand_HasFlags(t *testing.T) {
	cmd := NewPruneCommand()
	for _, name := range []string{"config", "dry-run", "yes", "output"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("NewPruneCommand(): missing --%s flag", name)
		}
	}
}
