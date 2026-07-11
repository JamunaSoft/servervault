package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JamunaSoft/servervault/internal/lock"
)

func TestNewBackupCommand_MissingExplicitFile(t *testing.T) {
	cmd := NewBackupCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--config", filepath.Join(t.TempDir(), "missing.yaml")})

	err := cmd.Execute()
	if got := ExitCode(err); got != 2 {
		t.Fatalf("ExitCode(Execute()) = %d, want 2 (stderr: %s)", got, errOut.String())
	}
}

func TestNewBackupCommand_InvalidConfig(t *testing.T) {
	configPath := writeTestConfig(t, `
retention:
  keep_daily: -1
`)

	cmd := NewBackupCommand()
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

func TestNewBackupCommand_LockBusy(t *testing.T) {
	// A valid config, but with the backup lock already held: Engine.Run
	// must fail fast on the lock, before ever needing a real Restic
	// repository or PostgreSQL connection to be reachable -- which lets
	// this test run without either binary actually working.
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "backup.lock")

	held, err := lock.Acquire(lockPath)
	if err != nil {
		t.Fatalf("setup: acquire lock: %v", err)
	}
	defer held.Release()

	configPath := writeTestConfig(t, `
restic:
  repository: "sftp:user@host:/backups/servervault"
  password_file: "/nonexistent/restic-password"
backup:
  paths:
    - /tmp
  lock_file: "`+lockPath+`"
postgres:
  enabled: false
retention:
  keep_daily: 7
`)

	cmd := NewBackupCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--config", configPath})

	err = cmd.Execute()
	if got := ExitCode(err); got != 1 {
		t.Fatalf("ExitCode(Execute()) = %d, want 1 (stderr: %s)", got, errOut.String())
	}
	if !strings.Contains(errOut.String(), "already running") {
		t.Errorf("stderr = %q, want it to mention a backup is already running", errOut.String())
	}
}

func TestNewBackupCommand_HasConfigFlag(t *testing.T) {
	cmd := NewBackupCommand()
	if cmd.Flags().Lookup("config") == nil {
		t.Error("NewBackupCommand(): missing --config flag")
	}
}
