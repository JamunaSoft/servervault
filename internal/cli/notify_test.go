package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/JamunaSoft/servervault/internal/lock"
	"github.com/JamunaSoft/servervault/internal/notify"
)

// TestNewBackupCommand_NotifiesOnFailureWhenConfigured proves
// wrapEventSinkWithNotify is actually wired into servervault backup's
// real event store, not just unit-tested in isolation: a real,
// writable state_dir plus notify.enabled means a lock-busy failure
// (the same scenario TestNewBackupCommand_LockBusy exercises) also
// posts to a real HTTP server standing in for the configured webhook.
func TestNewBackupCommand_NotifiesOnFailureWhenConfigured(t *testing.T) {
	var mu sync.Mutex
	var received []notify.Payload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p notify.Payload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Errorf("decode webhook payload: %v", err)
		}
		mu.Lock()
		received = append(received, p)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

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
notify:
  enabled: true
  webhook_url: "`+srv.URL+`"
`)

	cmd := NewBackupCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--config", configPath})

	if err := cmd.Execute(); ExitCode(err) != 1 {
		t.Fatalf("ExitCode(Execute()) = %d, want 1 (stderr: %s)", ExitCode(err), errOut.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("webhook received %d requests, want 1", len(received))
	}
	if received[0].EventType != "job.failed" {
		t.Errorf("EventType = %q, want job.failed", received[0].EventType)
	}
	if received[0].ErrorCategory != "lock" {
		t.Errorf("ErrorCategory = %q, want lock", received[0].ErrorCategory)
	}
}

// TestNewBackupCommand_DoesNotNotifyWhenDisabled proves the converse:
// the same failure, with notify.enabled left at its default (false),
// never calls the webhook at all.
func TestNewBackupCommand_DoesNotNotifyWhenDisabled(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

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

	if called {
		t.Error("webhook was called despite notify.enabled being false")
	}
}
