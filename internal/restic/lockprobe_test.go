//go:build integration && resticlock

// This file is the opt-in, best-effort Restic lock-conflict probe. It is
// gated behind an EXTRA build tag ("resticlock") on top of "integration",
// so it never runs as part of the required integration suite -- only a
// dedicated, manual-or-scheduled CI job builds with
// `-tags=integration,resticlock` (see
// .github/workflows/restic-lock-probe.yml and docs/testing.md).
//
// WARNING -- version-sensitive: this test polls the local repository's
// on-disk locks/ directory to know precisely when a background `restic
// backup` has acquired restic's internal repository lock. The locks/
// directory layout is an implementation detail of restic's `local:`
// backend, not a documented/stable API, and restic's own lock-retry
// behavior (whether a conflicting operation fails fast or retries for a
// while before giving up) is not something ServerVault controls or
// assumes a specific version of. This is why the test skips rather than
// fails whenever the environment doesn't cooperate, and why it stays out
// of the required suite -- see internal/restic/exitcode_test.go and
// restic_test.go's TestRepository_Check_LockConflictIsClassifiedDeterministically
// for the deterministic, fake-Runner test of the same *classification*
// logic, which IS part of the required unit test suite.
package restic

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JamunaSoft/servervault/internal/execx"
	"github.com/JamunaSoft/servervault/internal/testsupport"
)

func TestResticLockProbe_ConcurrentBackupIsClassifiedAsLockFailed(t *testing.T) {
	testsupport.RequireRestic(t)

	cfg := testsupport.NewResticRepository(t)
	repo := New(execx.DefaultRunner{}, cfg)

	repoDir := strings.TrimPrefix(cfg.Repository, "local:")
	locksDir := filepath.Join(repoDir, "locks")

	// Large enough that the first backup holds restic's lock for a
	// real window, not just a few milliseconds.
	payloadFile := filepath.Join(t.TempDir(), "payload.bin")
	writeRandomFile(t, payloadFile, 128<<20) // 128 MiB

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	firstDone := make(chan error, 1)
	go func() {
		_, err := repo.Backup(ctx, BackupOptions{Paths: []string{payloadFile}, Tags: []string{"lock-probe-first"}})
		firstDone <- err
	}()

	if !waitForLockFile(t, locksDir, 15*time.Second) {
		t.Skip("first backup completed before a lock file was observed on disk; cannot reliably provoke a conflict in this environment")
	}

	_, secondErr := repo.Backup(ctx, BackupOptions{Paths: []string{payloadFile}, Tags: []string{"lock-probe-second"}})

	if firstErr := <-firstDone; firstErr != nil {
		t.Fatalf("first (lock-holding) backup failed unexpectedly: %v", firstErr)
	}

	if secondErr == nil {
		t.Skip("second backup did not conflict with the first -- restic's lock-retry behavior may have absorbed it; this probe is best-effort and version-sensitive")
	}
	var exitErr *ExitError
	if !errors.As(secondErr, &exitErr) {
		t.Fatalf("second backup error = %v, want it to unwrap to *ExitError", secondErr)
	}
	if exitErr.Code != ExitLockFailed {
		t.Skipf("second backup failed with %v (not ExitLockFailed) -- restic's exact behavior here is version-sensitive; full error: %v", exitErr.Code, secondErr)
	}
}

// waitForLockFile polls locksDir (a local: backend repository's on-disk
// lock directory) until at least one entry appears, so the conflicting
// backup below reliably overlaps the lock-holding one instead of racing
// on a guessed sleep duration. This kind of on-disk inspection is
// deliberately confined to this opt-in, non-required probe -- see the
// file-level warning above.
func waitForLockFile(t *testing.T, locksDir string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(locksDir)
		if err == nil && len(entries) > 0 {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func writeRandomFile(t *testing.T, path string, size int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create payload file: %v", err)
	}
	defer f.Close()
	if _, err := io.CopyN(f, rand.Reader, size); err != nil {
		t.Fatalf("write payload file: %v", err)
	}
}
