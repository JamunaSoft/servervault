//go:build integration

package backup

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/execx"
	"github.com/JamunaSoft/servervault/internal/lock"
	"github.com/JamunaSoft/servervault/internal/restic"
	"github.com/JamunaSoft/servervault/internal/testsupport"
)

// integrationConfig starts from the same fixture backup_test.go's unit
// tests use (temp Root/LockFile, a single backup path) and swaps in a
// real, freshly-initialized local Restic repository.
func integrationConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := testConfig(t)
	cfg.Restic = testsupport.NewResticRepository(t)
	return cfg
}

func assertNoDumpFilesLeftBehind(t *testing.T, cfg *config.Config) {
	t.Helper()
	remaining, _ := filepath.Glob(filepath.Join(cfg.Backup.Root, "postgresql", "*"))
	if len(remaining) != 0 {
		t.Errorf("dump file(s) left behind: %v", remaining)
	}
}

func assertLockReleased(t *testing.T, cfg *config.Config) {
	t.Helper()
	held, _, err := lock.Status(cfg.Backup.LockFile)
	if err != nil {
		t.Errorf("lock.Status(): unexpected error: %v", err)
	}
	if held {
		t.Error("lock is still held after Run() returned")
	}
}

func TestIntegration_Run_Success_PostgresDisabled(t *testing.T) {
	cfg := integrationConfig(t)
	cfg.Postgres.Enabled = false

	engine, err := New(cfg, nil, execx.DefaultRunner{})
	if err != nil {
		t.Fatalf("New(): unexpected error: %v", err)
	}

	result, err := engine.Run(context.Background())
	if err != nil {
		t.Fatalf("Run(): unexpected error: %v", err)
	}
	if result.SnapshotID == "" {
		t.Error("Result.SnapshotID is empty")
	}
	if result.DumpBytes != 0 {
		t.Errorf("Result.DumpBytes = %d, want 0 (Postgres disabled)", result.DumpBytes)
	}

	assertNoDumpFilesLeftBehind(t, cfg)
	assertLockReleased(t, cfg)

	repo := restic.New(execx.DefaultRunner{}, cfg.Restic)
	snapshots, err := repo.Snapshots(context.Background(), restic.SnapshotsOptions{})
	if err != nil {
		t.Fatalf("Snapshots(): unexpected error: %v", err)
	}
	if len(snapshots) != 1 {
		t.Errorf("Snapshots() = %d entries, want 1", len(snapshots))
	}
}

func TestIntegration_Run_Success_PostgresEnabled(t *testing.T) {
	cfg := integrationConfig(t)
	cfg.Postgres = testsupport.NewPostgresDatabase(t)

	engine, err := New(cfg, nil, execx.DefaultRunner{})
	if err != nil {
		t.Fatalf("New(): unexpected error: %v", err)
	}

	result, err := engine.Run(context.Background())
	if err != nil {
		t.Fatalf("Run(): unexpected error: %v", err)
	}
	if result.SnapshotID == "" {
		t.Error("Result.SnapshotID is empty")
	}
	if result.DumpBytes == 0 {
		t.Error("Result.DumpBytes = 0, want > 0 (Postgres enabled, should have dumped something)")
	}

	assertNoDumpFilesLeftBehind(t, cfg)
	assertLockReleased(t, cfg)
}

func TestIntegration_Run_PostgresConnectivityFailure_CleansUp(t *testing.T) {
	// Deliberately does NOT require restic: Ping fails before restic is
	// ever reached (already proven by fake-based
	// TestEngine_Run_PingFailureStopsBeforeDump in backup_test.go), so
	// this test only needs a structurally-valid (never actually
	// invoked) Restic config -- keeping it runnable in a
	// postgres-only CI environment that doesn't install restic at all.
	testsupport.RequirePostgresBinaries(t)

	cfg := testConfig(t)
	cfg.Restic = config.ResticConfig{
		Repository:   "local:" + filepath.Join(t.TempDir(), "never-touched"),
		PasswordFile: filepath.Join(t.TempDir(), "unused-password"),
	}
	cfg.Postgres = config.PostgresConfig{
		Enabled:  true,
		Database: "servervault_test_definitely_does_not_exist",
		User:     testsupport.TestPostgresUser(),
	}

	engine, err := New(cfg, nil, execx.DefaultRunner{})
	if err != nil {
		t.Fatalf("New(): unexpected error: %v", err)
	}

	_, err = engine.Run(context.Background())
	if err == nil {
		t.Fatal("Run() with an unreachable database: want an error, got nil")
	}

	assertNoDumpFilesLeftBehind(t, cfg)
	assertLockReleased(t, cfg)
}

func TestIntegration_Run_ResticWrongPassword_CleansUp(t *testing.T) {
	cfg := integrationConfig(t)
	cfg.Postgres.Enabled = false

	// Repository is real and already initialized (by integrationConfig);
	// point at a different, wrong password file for this run only.
	wrongPasswordFile := filepath.Join(t.TempDir(), "wrong-password")
	if err := os.WriteFile(wrongPasswordFile, []byte("not-the-real-password"), 0o600); err != nil {
		t.Fatalf("write wrong password file: %v", err)
	}
	cfg.Restic.PasswordFile = wrongPasswordFile

	engine, err := New(cfg, nil, execx.DefaultRunner{})
	if err != nil {
		t.Fatalf("New(): unexpected error: %v", err)
	}

	_, err = engine.Run(context.Background())
	if err == nil {
		t.Fatal("Run() with the wrong Restic password: want an error, got nil")
	}
	var exitErr *restic.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %v, want it to unwrap to *restic.ExitError", err)
	}
	if exitErr.Code != restic.ExitWrongPassword {
		t.Errorf("Code = %v, want ExitWrongPassword", exitErr.Code)
	}

	assertNoDumpFilesLeftBehind(t, cfg)
	assertLockReleased(t, cfg)
}

func TestIntegration_Run_Cancellation_CleansUp(t *testing.T) {
	cfg := integrationConfig(t)
	cfg.Postgres.Enabled = false

	// A large-enough payload that canceling shortly after starting Run()
	// reliably lands while restic is still working, not after it's
	// already finished.
	payloadDir := t.TempDir()
	writeRandomFile(t, filepath.Join(payloadDir, "payload.bin"), 200<<20) // 200 MiB
	cfg.Backup.Paths = []string{payloadDir}

	engine, err := New(cfg, nil, execx.DefaultRunner{})
	if err != nil {
		t.Fatalf("New(): unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = engine.Run(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Run() with a canceled context: want an error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want it to wrap context.DeadlineExceeded or context.Canceled", err)
	}
	// Sanity: this should have been cut short, not run to completion
	// (200 MiB would normally take noticeably longer than the 300ms
	// deadline plus the grace period for SIGTERM to land).
	if elapsed > 10*time.Second {
		t.Errorf("Run() took %s to return after cancellation, want it to return promptly", elapsed)
	}

	assertNoDumpFilesLeftBehind(t, cfg)
	assertLockReleased(t, cfg)
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
