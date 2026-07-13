package cli

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JamunaSoft/servervault/internal/job"
	"github.com/JamunaSoft/servervault/internal/lock"
	_ "modernc.org/sqlite"
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

// TestNewBackupCommand_CreatesJobRecordWhenStateDirIsWritable exercises
// the full CLI wiring (not just Engine.Run in isolation, as
// internal/backup's own tests do): with a real, writable state_dir, a
// lock-busy backup attempt still creates and fails a real job record on
// disk, proving job.Open/WithJobStore/Reconcile are actually wired
// together correctly end to end, not just individually.
func TestNewBackupCommand_CreatesJobRecordWhenStateDirIsWritable(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "backup.lock")
	stateDir := filepath.Join(dir, "state")

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
state_dir: "`+stateDir+`"
`)

	cmd := NewBackupCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--config", configPath})

	if err := cmd.Execute(); ExitCode(err) != 1 {
		t.Fatalf("ExitCode(Execute()) = %d, want 1 (stderr: %s)", ExitCode(err), errOut.String())
	}

	dbPath := filepath.Join(stateDir, "jobs.db")
	jobs, err := job.Open(dbPath)
	if err != nil {
		t.Fatalf("job.Open: %v (state_dir was never created, so the CLI never wired up job tracking)", err)
	}

	// Reconcile must find nothing left in progress -- the lock-busy job
	// should already have ended in a terminal state, not gotten stuck.
	n, err := jobs.Reconcile(context.Background())
	if err != nil {
		jobs.Close()
		t.Fatalf("Reconcile: %v", err)
	}
	if n != 0 {
		t.Errorf("Reconcile found %d unreconciled in-progress job(s); the lock-busy job should already have ended in a terminal state", n)
	}
	jobs.Close()

	// Reconcile alone can't distinguish "one job, already terminal" from
	// "zero jobs ever created" -- count rows directly to rule out the
	// latter and prove the CLI really did wire job creation through.
	count := countJobRows(t, dbPath)
	if count != 1 {
		t.Errorf("jobs table has %d row(s), want exactly 1 (the lock-busy attempt)", count)
	}
}

func countJobRows(t *testing.T, dbPath string) int {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open jobs.db directly: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM jobs;`).Scan(&count); err != nil {
		t.Fatalf("count jobs rows: %v", err)
	}
	return count
}
