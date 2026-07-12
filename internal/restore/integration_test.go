//go:build integration

package restore

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JamunaSoft/servervault/internal/backup"
	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/event"
	"github.com/JamunaSoft/servervault/internal/execx"
	"github.com/JamunaSoft/servervault/internal/job"
	"github.com/JamunaSoft/servervault/internal/postgres"
	"github.com/JamunaSoft/servervault/internal/restic"
	"github.com/JamunaSoft/servervault/internal/testsupport"
)

// integrationConfig builds a config rooted entirely under t.TempDir(),
// with a real, freshly-initialized local Restic repository -- the same
// pattern internal/backup's own integration suite uses (see that
// package's integration_test.go), so a real snapshot created via
// internal/backup.Engine.Run here is restorable through internal/restore
// against the exact same repository shape production code produces.
func integrationConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()

	cfg := config.Defaults()
	cfg.Restic = testsupport.NewResticRepository(t)
	cfg.Backup.Root = filepath.Join(dir, "backups")
	cfg.Backup.LockFile = filepath.Join(dir, "backup.lock")
	cfg.Backup.ExcludeFile = newTestExcludeFile(t)
	cfg.Restore.StagingRoot = filepath.Join(dir, "restore")
	cfg.Restore.LockFile = filepath.Join(dir, "restore.lock")
	cfg.Restore.TempDatabasePrefix = "servervault_restore_"
	cfg.StateDir = filepath.Join(dir, "state")
	cfg.Postgres.Enabled = false

	payloadDir := filepath.Join(dir, "payload")
	if err := os.MkdirAll(payloadDir, 0o755); err != nil {
		t.Fatalf("setup: create payload dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(payloadDir, "hello.txt"), []byte("hello from servervault restore integration test\n"), 0o600); err != nil {
		t.Fatalf("setup: write payload file: %v", err)
	}
	cfg.Backup.Paths = []string{payloadDir}

	return cfg
}

func newTestExcludeFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "excludes.txt")
	if err := os.WriteFile(path, []byte("# servervault integration test -- intentionally empty\n"), 0o600); err != nil {
		t.Fatalf("write test exclude file: %v", err)
	}
	return path
}

// createRealSnapshot runs a real backup end to end (real restic, real
// PostgreSQL when cfg.Postgres.Enabled) and returns the resulting
// snapshot ID -- the fixture every test in this file restores from.
func createRealSnapshot(t *testing.T, cfg *config.Config) string {
	t.Helper()
	engine, err := backup.New(cfg, nil, execx.DefaultRunner{})
	if err != nil {
		t.Fatalf("setup: backup.New: %v", err)
	}
	result, err := engine.Run(context.Background())
	if err != nil {
		t.Fatalf("setup: create real snapshot: %v", err)
	}
	return result.SnapshotID
}

func newExecutorForTest(t *testing.T, cfg *config.Config, pg PostgresClient) (*Executor, *restic.Repository, *event.InMemorySink) {
	t.Helper()
	repo := restic.New(execx.DefaultRunner{}, cfg.Restic)

	jobStore, err := job.Open(filepath.Join(cfg.StateDir, "jobs.db"))
	if err != nil {
		t.Fatalf("setup: job.Open: %v", err)
	}
	t.Cleanup(func() { jobStore.Close() })

	sink := &event.InMemorySink{}

	x, err := NewExecutor(repo, pg, cfg, jobStore, sink, nil)
	if err != nil {
		t.Fatalf("setup: NewExecutor: %v", err)
	}
	return x, repo, sink
}

func TestIntegration_Restore_Files_Success(t *testing.T) {
	testsupport.RequireRestic(t)
	cfg := integrationConfig(t)
	snapshotID := createRealSnapshot(t, cfg)

	x, repo, sink := newExecutorForTest(t, cfg, nil)
	planner, err := NewPlanner(repo, cfg)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	plan, err := planner.Plan(context.Background(), PlanOptions{SnapshotID: snapshotID, Target: TargetFiles})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !plan.BytesKnown || plan.ExpectedFiles == 0 {
		t.Fatalf("plan stats not derived from real repository metadata: %+v", plan)
	}

	result, err := x.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.FilesRestored == 0 {
		t.Error("expected at least one file restored")
	}

	restored, err := os.ReadFile(filepath.Join(plan.Destination, cfg.Backup.Paths[0], "hello.txt"))
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(restored) != "hello from servervault restore integration test\n" {
		t.Errorf("restored content = %q, want the original payload", restored)
	}

	if !hasEventType(sink.Events(), event.TypeRestoreCompleted) {
		t.Error("expected a restore.completed event")
	}
}

func TestIntegration_Restore_Files_DryRunPerformsNoWrites(t *testing.T) {
	testsupport.RequireRestic(t)
	cfg := integrationConfig(t)
	snapshotID := createRealSnapshot(t, cfg)

	repo := restic.New(execx.DefaultRunner{}, cfg.Restic)
	planner, err := NewPlanner(repo, cfg)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	plan, err := planner.Plan(context.Background(), PlanOptions{SnapshotID: snapshotID, Target: TargetFiles})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// Plan alone (without ever calling Execute) must not have created
	// the destination or written anything under StagingRoot.
	if _, err := os.Stat(plan.Destination); !os.IsNotExist(err) {
		t.Errorf("Plan created the destination directory; dry-run must perform no writes (stat err = %v)", err)
	}
	entries, _ := os.ReadDir(cfg.Restore.StagingRoot)
	if len(entries) != 0 {
		t.Errorf("StagingRoot has %d entries after Plan alone, want 0", len(entries))
	}
}

func TestIntegration_Restore_Files_ExistingDestinationRejected(t *testing.T) {
	testsupport.RequireRestic(t)
	cfg := integrationConfig(t)
	snapshotID := createRealSnapshot(t, cfg)

	x, repo, _ := newExecutorForTest(t, cfg, nil)
	planner, _ := NewPlanner(repo, cfg)
	plan, err := planner.Plan(context.Background(), PlanOptions{SnapshotID: snapshotID, Target: TargetFiles})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if err := os.MkdirAll(plan.Destination, 0o700); err != nil {
		t.Fatalf("setup: pre-create destination: %v", err)
	}

	_, err = x.Execute(context.Background(), plan)
	var stale *ErrPlanStale
	if !errors.As(err, &stale) {
		t.Fatalf("Execute error = %v, want *ErrPlanStale", err)
	}
}

func TestIntegration_Restore_Files_InvalidSnapshotID(t *testing.T) {
	testsupport.RequireRestic(t)
	cfg := integrationConfig(t)
	createRealSnapshot(t, cfg) // repository must be non-empty and reachable

	repo := restic.New(execx.DefaultRunner{}, cfg.Restic)
	planner, _ := NewPlanner(repo, cfg)

	_, err := planner.Plan(context.Background(), PlanOptions{SnapshotID: "0000000000000000000000000000000000000000000000000000000000000000", Target: TargetFiles})
	if err == nil {
		t.Fatal("Plan with a nonexistent snapshot ID should fail")
	}
}

// TestIntegration_Restore_Files_Cancellation mirrors internal/backup's
// own cancellation integration test: a large-enough real payload that a
// short deadline reliably lands while the real restic subprocess is
// still restoring, not after it has already finished.
func TestIntegration_Restore_Files_Cancellation(t *testing.T) {
	testsupport.RequireRestic(t)
	cfg := integrationConfig(t)

	payloadDir := filepath.Join(t.TempDir(), "big-payload")
	if err := os.MkdirAll(payloadDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	writeRandomFile(t, filepath.Join(payloadDir, "payload.bin"), 200<<20) // 200 MiB
	cfg.Backup.Paths = []string{payloadDir}

	snapshotID := createRealSnapshot(t, cfg)

	x, repo, sink := newExecutorForTest(t, cfg, nil)
	planner, _ := NewPlanner(repo, cfg)
	plan, err := planner.Plan(context.Background(), PlanOptions{SnapshotID: snapshotID, Target: TargetFiles})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = x.Execute(ctx, plan)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Execute with a short deadline: want an error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("Execute error = %v, want it to wrap context.DeadlineExceeded or context.Canceled", err)
	}
	if elapsed > 10*time.Second {
		t.Errorf("Execute took %s to return after cancellation, want it to return promptly", elapsed)
	}

	if !hasEventType(sink.Events(), event.TypeJobCancelled) {
		t.Errorf("expected a job.cancelled event, got %v", eventTypes(sink.Events()))
	}
}

func TestIntegration_Restore_TempDB_Success(t *testing.T) {
	testsupport.RequireRestic(t)
	testsupport.RequirePostgresBinaries(t)

	cfg := integrationConfig(t)
	cfg.Postgres = testsupport.NewPostgresDatabase(t)
	liveDatabase := cfg.Postgres.Database

	snapshotID := createRealSnapshot(t, cfg)

	pgClient := postgres.New(execx.DefaultRunner{}, cfg.Postgres)
	x, repo, sink := newExecutorForTest(t, cfg, pgClient)
	planner, _ := NewPlanner(repo, cfg)

	plan, err := planner.Plan(context.Background(), PlanOptions{SnapshotID: snapshotID, Target: TargetTempDB})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.TempDatabaseName == liveDatabase {
		t.Fatal("temp database name must never equal the live database name")
	}

	result, err := x.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	t.Cleanup(func() {
		_ = pgClient.DropDatabase(context.Background(), plan.TempDatabaseName)
	})

	if result.Destination != plan.TempDatabaseName {
		t.Errorf("result.Destination = %q, want %q", result.Destination, plan.TempDatabaseName)
	}

	// Verify the restored data is actually queryable in the new
	// database -- internal/postgres.testsupport seeds every test
	// database with a servervault_probe table/row.
	exists, err := pgClient.DatabaseExists(context.Background(), plan.TempDatabaseName)
	if err != nil {
		t.Fatalf("DatabaseExists: %v", err)
	}
	if !exists {
		t.Fatal("temporary database does not exist after a successful restore")
	}
	if err := pgClient.PingDatabase(context.Background(), plan.TempDatabaseName); err != nil {
		t.Errorf("PingDatabase(temp): %v", err)
	}

	// The live database (the one the backup was taken from) must be
	// completely untouched by the restore.
	liveExists, err := pgClient.DatabaseExists(context.Background(), liveDatabase)
	if err != nil {
		t.Fatalf("DatabaseExists(live): %v", err)
	}
	if !liveExists {
		t.Fatal("live database no longer exists after restore -- restore must never touch the live database")
	}

	if !hasEventType(sink.Events(), event.TypeRestoreCompleted) {
		t.Errorf("expected a restore.completed event, got %v", eventTypes(sink.Events()))
	}
}

func TestIntegration_Restore_TempDB_NameCollisionRevalidation(t *testing.T) {
	testsupport.RequireRestic(t)
	testsupport.RequirePostgresBinaries(t)

	cfg := integrationConfig(t)
	cfg.Postgres = testsupport.NewPostgresDatabase(t)
	snapshotID := createRealSnapshot(t, cfg)

	pgClient := postgres.New(execx.DefaultRunner{}, cfg.Postgres)
	x, repo, _ := newExecutorForTest(t, cfg, pgClient)
	planner, _ := NewPlanner(repo, cfg)

	plan, err := planner.Plan(context.Background(), PlanOptions{SnapshotID: snapshotID, Target: TargetTempDB})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// Simulate the exact predicted temp database name having been taken
	// in the gap between Plan and Execute.
	if err := pgClient.CreateDatabase(context.Background(), plan.TempDatabaseName); err != nil {
		t.Fatalf("setup: pre-create temp database: %v", err)
	}
	t.Cleanup(func() { _ = pgClient.DropDatabase(context.Background(), plan.TempDatabaseName) })

	_, err = x.Execute(context.Background(), plan)
	var stale *ErrPlanStale
	if !errors.As(err, &stale) {
		t.Fatalf("Execute error = %v, want *ErrPlanStale", err)
	}
}

func TestIntegration_Restore_TempDB_MissingDumpRejected(t *testing.T) {
	testsupport.RequireRestic(t)
	cfg := integrationConfig(t)
	cfg.Postgres.Enabled = false // backup with no database enabled -> snapshot has no dump file
	snapshotID := createRealSnapshot(t, cfg)

	// Re-enable Postgres only for planning the restore -- the snapshot
	// itself genuinely has no dump file in it, which is exactly the case
	// this test exercises.
	cfg.Postgres.Enabled = true
	cfg.Postgres.Database = "app_production"

	repo := restic.New(execx.DefaultRunner{}, cfg.Restic)
	planner, _ := NewPlanner(repo, cfg)

	_, err := planner.Plan(context.Background(), PlanOptions{SnapshotID: snapshotID, Target: TargetTempDB})
	if !errors.Is(err, ErrDumpNotFound) {
		t.Errorf("Plan error = %v, want ErrDumpNotFound", err)
	}
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
