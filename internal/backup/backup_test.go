package backup

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/lock"
	"github.com/JamunaSoft/servervault/internal/postgres"
	"github.com/JamunaSoft/servervault/internal/restic"
)

// fakeDumper and fakeBacker let backup_test.go exercise Engine.Run's
// orchestration logic (lock/verify-gate/cleanup ordering) without
// depending on internal/postgres or internal/restic's own subprocess
// behavior, which is tested separately in their own packages.
type fakeDumper struct {
	pingErr   error
	dumpMeta  postgres.Metadata
	dumpErr   error
	verifyErr error

	pingCalled, dumpCalled, verifyCalled bool
	verifyPath                           string
}

func (f *fakeDumper) Ping(ctx context.Context) error {
	f.pingCalled = true
	return f.pingErr
}
func (f *fakeDumper) Dump(ctx context.Context, dir string) (postgres.Metadata, error) {
	f.dumpCalled = true
	return f.dumpMeta, f.dumpErr
}
func (f *fakeDumper) VerifyDump(ctx context.Context, path string) error {
	f.verifyCalled = true
	f.verifyPath = path
	return f.verifyErr
}

type fakeBacker struct {
	summary restic.Summary
	err     error
	called  bool
	gotOpts restic.BackupOptions
}

func (f *fakeBacker) Backup(ctx context.Context, opts restic.BackupOptions) (restic.Summary, error) {
	f.called = true
	f.gotOpts = opts
	return f.summary, f.err
}

func testEngine(t *testing.T, cfg *config.Config, dumper Dumper, backer Backer) *Engine {
	t.Helper()
	return &Engine{
		cfg:      cfg,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		postgres: dumper,
		restic:   backer,
	}
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.Restic.Repository = "sftp:user@host:/backups/servervault"
	cfg.Backup.Paths = []string{"/var/www"}
	cfg.Backup.Root = dir
	cfg.Backup.LockFile = filepath.Join(dir, "backup.lock")
	cfg.Postgres.Database = "app_production"
	return cfg
}

func TestEngine_Run_Success(t *testing.T) {
	cfg := testConfig(t)
	dumpPath := filepath.Join(t.TempDir(), "app_production.dump.zst")
	if err := os.WriteFile(dumpPath, []byte("dump"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	dumper := &fakeDumper{dumpMeta: postgres.Metadata{Path: dumpPath, Bytes: 4}}
	backer := &fakeBacker{summary: restic.Summary{SnapshotID: "abc123", FilesNew: 3}}
	e := testEngine(t, cfg, dumper, backer)

	result, err := e.Run(context.Background())
	if err != nil {
		t.Fatalf("Run(): unexpected error: %v", err)
	}

	if !dumper.pingCalled || !dumper.dumpCalled || !dumper.verifyCalled {
		t.Errorf("expected Ping, Dump, and VerifyDump to all be called: %+v", dumper)
	}
	if dumper.verifyPath != dumpPath {
		t.Errorf("VerifyDump path = %q, want %q", dumper.verifyPath, dumpPath)
	}
	if !backer.called {
		t.Fatal("expected restic.Backup to be called")
	}
	if !contains(backer.gotOpts.Paths, dumpPath) {
		t.Errorf("restic backup paths = %v, want it to include the dump path %q", backer.gotOpts.Paths, dumpPath)
	}
	if !contains(backer.gotOpts.Paths, "/var/www") {
		t.Errorf("restic backup paths = %v, want it to include configured backup.paths", backer.gotOpts.Paths)
	}
	if !contains(backer.gotOpts.Tags, "servervault") {
		t.Errorf("restic backup tags = %v, want it to include \"servervault\"", backer.gotOpts.Tags)
	}

	if result.SnapshotID != "abc123" || result.FilesNew != 3 || result.DumpBytes != 4 {
		t.Errorf("Result = %+v, unexpected values", result)
	}
	if result.FinishedAt.Before(result.StartedAt) {
		t.Error("Result.FinishedAt is before Result.StartedAt")
	}

	// The dump file must be removed after a successful run -- it's
	// already safely in the repository.
	if _, err := os.Stat(dumpPath); !os.IsNotExist(err) {
		t.Errorf("dump file %q still exists after a successful run", dumpPath)
	}

	// The lock must be released -- a second Run() must succeed.
	if held, _, err := lock.Status(cfg.Backup.LockFile); err != nil || held {
		t.Errorf("lock still held after Run() returned: held=%v err=%v", held, err)
	}
}

func TestEngine_Run_LockBusy(t *testing.T) {
	cfg := testConfig(t)

	held, err := lock.Acquire(cfg.Backup.LockFile)
	if err != nil {
		t.Fatalf("setup: acquire lock: %v", err)
	}
	defer held.Release()

	dumper := &fakeDumper{}
	backer := &fakeBacker{}
	e := testEngine(t, cfg, dumper, backer)

	_, err = e.Run(context.Background())
	if !errors.Is(err, lock.ErrLocked) {
		t.Fatalf("Run() with the lock already held: error = %v, want lock.ErrLocked", err)
	}
	if dumper.pingCalled || backer.called {
		t.Error("Run() with a busy lock must not call Ping or Backup at all")
	}
}

func TestEngine_Run_PingFailureStopsBeforeDump(t *testing.T) {
	cfg := testConfig(t)
	dumper := &fakeDumper{pingErr: errors.New("connection refused")}
	backer := &fakeBacker{}
	e := testEngine(t, cfg, dumper, backer)

	_, err := e.Run(context.Background())
	if err == nil {
		t.Fatal("Run() with a ping failure: want an error, got nil")
	}
	if dumper.dumpCalled || dumper.verifyCalled || backer.called {
		t.Error("Run() with a ping failure must not call Dump, VerifyDump, or Backup")
	}
	if held, _, statusErr := lock.Status(cfg.Backup.LockFile); statusErr != nil || held {
		t.Errorf("lock still held after a ping failure: held=%v err=%v", held, statusErr)
	}
}

func TestEngine_Run_DumpFailureCleansUpAndSkipsRestic(t *testing.T) {
	cfg := testConfig(t)
	partialPath := filepath.Join(t.TempDir(), "partial.dump.zst")
	if err := os.WriteFile(partialPath, []byte("partial"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	dumper := &fakeDumper{
		dumpMeta: postgres.Metadata{Path: partialPath}, // Dump reports the path even on failure
		dumpErr:  errors.New("pg_dump: connection reset"),
	}
	backer := &fakeBacker{}
	e := testEngine(t, cfg, dumper, backer)

	_, err := e.Run(context.Background())
	if err == nil {
		t.Fatal("Run() with a dump failure: want an error, got nil")
	}
	if dumper.verifyCalled || backer.called {
		t.Error("Run() with a dump failure must not call VerifyDump or Backup")
	}
	if _, statErr := os.Stat(partialPath); !os.IsNotExist(statErr) {
		t.Error("partial dump file was not cleaned up after a dump failure")
	}
}

func TestEngine_Run_VerifyFailureNeverCallsRestic(t *testing.T) {
	// The single most important safety property in this package: a
	// failed dump verification must never let a backup reach Restic.
	cfg := testConfig(t)
	dumpPath := filepath.Join(t.TempDir(), "corrupt.dump.zst")
	if err := os.WriteFile(dumpPath, []byte("corrupt"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	dumper := &fakeDumper{
		dumpMeta:  postgres.Metadata{Path: dumpPath},
		verifyErr: errors.New("pg_restore: unexpected end of file"),
	}
	backer := &fakeBacker{}
	e := testEngine(t, cfg, dumper, backer)

	_, err := e.Run(context.Background())
	if err == nil {
		t.Fatal("Run() with a verify failure: want an error, got nil")
	}
	if backer.called {
		t.Fatal("Run() with a verify failure called restic.Backup -- this must never happen")
	}
	if _, statErr := os.Stat(dumpPath); !os.IsNotExist(statErr) {
		t.Error("dump file was not cleaned up after a verify failure")
	}
}

func TestEngine_Run_ResticFailureCleansUp(t *testing.T) {
	cfg := testConfig(t)
	dumpPath := filepath.Join(t.TempDir(), "app.dump.zst")
	if err := os.WriteFile(dumpPath, []byte("dump"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	dumper := &fakeDumper{dumpMeta: postgres.Metadata{Path: dumpPath}}
	backer := &fakeBacker{err: errors.New("restic: repository lock failed")}
	e := testEngine(t, cfg, dumper, backer)

	_, err := e.Run(context.Background())
	if err == nil {
		t.Fatal("Run() with a restic failure: want an error, got nil")
	}
	if _, statErr := os.Stat(dumpPath); !os.IsNotExist(statErr) {
		t.Error("dump file was not cleaned up after a restic backup failure")
	}
	if held, _, statusErr := lock.Status(cfg.Backup.LockFile); statusErr != nil || held {
		t.Errorf("lock still held after a restic failure: held=%v err=%v", held, statusErr)
	}
}

func TestEngine_Run_PostgresDisabledSkipsDumpEntirely(t *testing.T) {
	cfg := testConfig(t)
	cfg.Postgres.Enabled = false

	backer := &fakeBacker{summary: restic.Summary{SnapshotID: "filesonly"}}
	e := testEngine(t, cfg, nil, backer) // postgres is nil, matching New()'s behavior when disabled

	result, err := e.Run(context.Background())
	if err != nil {
		t.Fatalf("Run(): unexpected error: %v", err)
	}
	if result.SnapshotID != "filesonly" {
		t.Errorf("SnapshotID = %q, want %q", result.SnapshotID, "filesonly")
	}
	if len(backer.gotOpts.Paths) != 1 || backer.gotOpts.Paths[0] != "/var/www" {
		t.Errorf("restic backup paths = %v, want exactly [\"/var/www\"] (no dump path)", backer.gotOpts.Paths)
	}
}

func TestNew_RejectsMissingLockFile(t *testing.T) {
	cfg := testConfig(t)
	cfg.Backup.LockFile = ""

	if _, err := New(cfg, nil, nil); err == nil {
		t.Fatal("New() with an empty LockFile: want an error, got nil")
	}
}

func TestNew_RejectsNilConfig(t *testing.T) {
	if _, err := New(nil, nil, nil); err == nil {
		t.Fatal("New(nil, ...): want an error, got nil")
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
