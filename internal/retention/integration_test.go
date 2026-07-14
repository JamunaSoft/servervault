//go:build integration

package retention

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/JamunaSoft/servervault/internal/backup"
	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/event"
	"github.com/JamunaSoft/servervault/internal/execx"
	"github.com/JamunaSoft/servervault/internal/job"
	"github.com/JamunaSoft/servervault/internal/lock"
	"github.com/JamunaSoft/servervault/internal/restic"
	"github.com/JamunaSoft/servervault/internal/testsupport"
)

// integrationConfig builds a config rooted entirely under t.TempDir(),
// with a real, freshly-initialized local Restic repository -- the same
// pattern internal/restore's own integration suite uses, so real
// snapshots created here via internal/backup.Engine.Run are pruned
// through internal/retention against the exact same repository shape
// production code produces.
func integrationConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()

	cfg := config.Defaults()
	cfg.Restic = testsupport.NewResticRepository(t)
	cfg.HostTag = "retention-integration-test"
	cfg.Backup.Root = filepath.Join(dir, "backups")
	cfg.Backup.LockFile = filepath.Join(dir, "backup.lock")
	cfg.Backup.ExcludeFile = newTestExcludeFile(t)
	cfg.Restore.StagingRoot = filepath.Join(dir, "restore")
	cfg.Restore.LockFile = filepath.Join(dir, "restore.lock")
	cfg.Restore.TempDatabasePrefix = "servervault_restore_"
	cfg.Retention.LockFile = filepath.Join(dir, "prune.lock")
	cfg.StateDir = filepath.Join(dir, "state")
	cfg.Postgres.Enabled = false

	payloadDir := filepath.Join(dir, "payload")
	if err := os.MkdirAll(payloadDir, 0o755); err != nil {
		t.Fatalf("setup: create payload dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(payloadDir, "hello.txt"), []byte("hello from servervault retention integration test\n"), 0o600); err != nil {
		t.Fatalf("setup: write payload file: %v", err)
	}
	cfg.Backup.Paths = []string{payloadDir}

	return cfg
}

func newTestExcludeFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "excludes.txt")
	if err := os.WriteFile(path, []byte("# servervault retention integration test -- intentionally empty\n"), 0o600); err != nil {
		t.Fatalf("write test exclude file: %v", err)
	}
	return path
}

// createRealSnapshot runs a real backup end to end and returns the
// resulting snapshot ID.
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

func openJobStore(t *testing.T, cfg *config.Config) *job.Store {
	t.Helper()
	s, err := job.Open(filepath.Join(cfg.StateDir, "jobs.db"))
	if err != nil {
		t.Fatalf("setup: job.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func openEventStore(t *testing.T, cfg *config.Config) *event.Store {
	t.Helper()
	s, err := event.Open(filepath.Join(cfg.StateDir, "events.db"))
	if err != nil {
		t.Fatalf("setup: event.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func realSnapshotCount(t *testing.T, cfg *config.Config, repo *restic.Repository) int {
	t.Helper()
	snapshots, err := repo.Snapshots(context.Background(), restic.SnapshotsOptions{Host: cfg.HostTag, Tags: []string{"servervault"}})
	if err != nil {
		t.Fatalf("Snapshots: %v", err)
	}
	return len(snapshots)
}

func TestIntegration_Retention_Plan_DoesNotModifyRepository(t *testing.T) {
	testsupport.RequireRestic(t)
	cfg := integrationConfig(t)
	// All three snapshots created below land in the same restic
	// "daily" bucket (same wall-clock day) -- keep_daily=1 keeps only
	// the most recent, so this deterministically produces a non-zero,
	// known removal count without needing to fabricate snapshot ages.
	cfg.Retention.KeepDaily = 1
	cfg.Retention.KeepWeekly = 0
	cfg.Retention.KeepMonthly = 0

	createRealSnapshot(t, cfg)
	createRealSnapshot(t, cfg)
	createRealSnapshot(t, cfg)

	repo := restic.New(execx.DefaultRunner{}, cfg.Restic)
	planner, err := NewPlanner(repo, cfg)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	before := realSnapshotCount(t, cfg, repo)

	plan, err := planner.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.CurrentSnapshotCount != 3 {
		t.Errorf("CurrentSnapshotCount = %d, want 3", plan.CurrentSnapshotCount)
	}
	if plan.RemoveCount != 2 {
		t.Errorf("RemoveCount = %d, want 2 (keep_daily=1 keeps only the most recent of 3 same-day snapshots)", plan.RemoveCount)
	}

	after := realSnapshotCount(t, cfg, repo)
	if after != before {
		t.Errorf("Plan modified the repository: %d snapshots before, %d after", before, after)
	}
}

func TestIntegration_Retention_Execute_RemovesSnapshots(t *testing.T) {
	testsupport.RequireRestic(t)
	cfg := integrationConfig(t)
	cfg.Retention.KeepDaily = 1
	cfg.Retention.KeepWeekly = 0
	cfg.Retention.KeepMonthly = 0

	createRealSnapshot(t, cfg)
	createRealSnapshot(t, cfg)
	createRealSnapshot(t, cfg)

	repo := restic.New(execx.DefaultRunner{}, cfg.Restic)
	planner, err := NewPlanner(repo, cfg)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	plan, err := planner.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	jobs := openJobStore(t, cfg)
	events := openEventStore(t, cfg)
	executor, err := NewExecutor(repo, cfg, jobs, events, nil)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	result, err := executor.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.RemovedCount != 2 {
		t.Errorf("RemovedCount = %d, want 2", result.RemovedCount)
	}

	remaining := realSnapshotCount(t, cfg, repo)
	if remaining != 1 {
		t.Errorf("remaining snapshots = %d, want 1", remaining)
	}

	recorded, err := jobs.Get(context.Background(), result.JobID)
	if err != nil {
		t.Fatalf("jobs.Get: %v", err)
	}
	if recorded.State != job.StateCompleted {
		t.Errorf("job state = %s, want %s", recorded.State, job.StateCompleted)
	}
	if recorded.Metadata.SnapshotsRemoved != 2 {
		t.Errorf("job metadata SnapshotsRemoved = %d, want 2", recorded.Metadata.SnapshotsRemoved)
	}
}

func TestIntegration_Retention_BelowMinimumSnapshots_Refuses(t *testing.T) {
	testsupport.RequireRestic(t)
	cfg := integrationConfig(t)
	cfg.Retention.KeepDaily = 1
	cfg.Retention.KeepWeekly = 0
	cfg.Retention.KeepMonthly = 0
	cfg.Retention.MinKeepTotal = 3 // pruning to 1 would violate this floor

	createRealSnapshot(t, cfg)
	createRealSnapshot(t, cfg)
	createRealSnapshot(t, cfg)

	repo := restic.New(execx.DefaultRunner{}, cfg.Restic)
	planner, err := NewPlanner(repo, cfg)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	before := realSnapshotCount(t, cfg, repo)

	_, err = planner.Plan(context.Background())
	if !errors.Is(err, ErrBelowMinimumSnapshots) {
		t.Fatalf("Plan error = %v, want ErrBelowMinimumSnapshots", err)
	}

	after := realSnapshotCount(t, cfg, repo)
	if after != before {
		t.Errorf("a refused plan modified the repository: %d before, %d after", before, after)
	}
}

func TestIntegration_Retention_MaxDeleteExceeded_Refuses(t *testing.T) {
	testsupport.RequireRestic(t)
	cfg := integrationConfig(t)
	cfg.Retention.KeepDaily = 1
	cfg.Retention.KeepWeekly = 0
	cfg.Retention.KeepMonthly = 0
	cfg.Retention.MaxDeleteCount = 1 // 2 would be removed, exceeding this

	createRealSnapshot(t, cfg)
	createRealSnapshot(t, cfg)
	createRealSnapshot(t, cfg)

	repo := restic.New(execx.DefaultRunner{}, cfg.Restic)
	planner, err := NewPlanner(repo, cfg)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	before := realSnapshotCount(t, cfg, repo)

	_, err = planner.Plan(context.Background())
	var maxErr *ErrMaxDeleteExceeded
	if !errors.As(err, &maxErr) {
		t.Fatalf("Plan error = %v, want *ErrMaxDeleteExceeded", err)
	}

	after := realSnapshotCount(t, cfg, repo)
	if after != before {
		t.Errorf("a refused plan modified the repository: %d before, %d after", before, after)
	}
}

func TestIntegration_Retention_LockConflict(t *testing.T) {
	testsupport.RequireRestic(t)
	cfg := integrationConfig(t)

	createRealSnapshot(t, cfg)

	repo := restic.New(execx.DefaultRunner{}, cfg.Restic)
	planner, err := NewPlanner(repo, cfg)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	plan, err := planner.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	pruneLock, ok, err := lock.TryAcquire(cfg.Retention.LockFile)
	if err != nil || !ok {
		t.Fatalf("setup: acquire prune lock: ok=%v err=%v", ok, err)
	}
	defer pruneLock.Release()

	jobs := openJobStore(t, cfg)
	events := openEventStore(t, cfg)
	executor, err := NewExecutor(repo, cfg, jobs, events, nil)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	_, err = executor.Execute(context.Background(), plan)
	if !errors.Is(err, lock.ErrLocked) {
		t.Errorf("Execute error = %v, want lock.ErrLocked", err)
	}

	remaining := realSnapshotCount(t, cfg, repo)
	if remaining != 1 {
		t.Errorf("a lock-refused Execute modified the repository: %d snapshots remain, want 1", remaining)
	}
}

func TestIntegration_Retention_Cancellation(t *testing.T) {
	testsupport.RequireRestic(t)
	cfg := integrationConfig(t)

	createRealSnapshot(t, cfg)
	createRealSnapshot(t, cfg)

	repo := restic.New(execx.DefaultRunner{}, cfg.Restic)
	planner, err := NewPlanner(repo, cfg)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	plan, err := planner.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	jobs := openJobStore(t, cfg)
	events := openEventStore(t, cfg)
	executor, err := NewExecutor(repo, cfg, jobs, events, nil)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	before := realSnapshotCount(t, cfg, repo)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := executor.Execute(ctx, plan); err == nil {
		t.Fatal("Execute with an already-cancelled context should fail")
	}

	after := realSnapshotCount(t, cfg, repo)
	if after != before {
		t.Errorf("a cancelled Execute modified the repository: %d before, %d after", before, after)
	}
}
