package restore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/event"
	"github.com/JamunaSoft/servervault/internal/job"
	"github.com/JamunaSoft/servervault/internal/lock"
	"github.com/JamunaSoft/servervault/internal/restic"
)

// testExecutorConfig returns a config rooted entirely under t.TempDir(),
// so every test is isolated and never touches a real system path.
func testExecutorConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.Backup.Paths = []string{filepath.Join(dir, "www")}
	cfg.Backup.Root = filepath.Join(dir, "backups")
	cfg.Backup.LockFile = filepath.Join(dir, "backup.lock")
	cfg.Restore.StagingRoot = filepath.Join(dir, "restore")
	cfg.Restore.LockFile = filepath.Join(dir, "restore.lock")
	cfg.Restore.TempDatabasePrefix = "servervault_restore_"
	cfg.Postgres.Enabled = true
	cfg.Postgres.Database = "app_production"
	return cfg
}

func newTestJobStore(t *testing.T) *job.Store {
	t.Helper()
	s, err := job.Open(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatalf("job.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newTestExecutor(t *testing.T, fr *fakeRestic, fp *fakePostgres, cfg *config.Config) (*Executor, *event.InMemorySink) {
	t.Helper()
	sink := &event.InMemorySink{}
	var pg PostgresClient
	if fp != nil {
		pg = fp
	}
	x, err := NewExecutor(fr, pg, cfg, newTestJobStore(t), sink, nil)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	return x, sink
}

func TestExecutor_Execute_Files_Success(t *testing.T) {
	cfg := testExecutorConfig(t)
	fr := &fakeRestic{
		listFiles: []restic.FileInfo{
			{Path: "file1", Type: "file", Size: 40},
			{Path: "file2", Type: "file", Size: 30},
			{Path: "file3", Type: "file", Size: 30},
		},
		restoreSummary:     restic.RestoreSummary{FilesRestored: 3, BytesRestored: 100},
		writeFileOnRestore: true,
	}
	x, sink := newTestExecutor(t, fr, nil, cfg)
	planner, err := NewPlanner(fr, cfg)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	plan, err := planner.Plan(context.Background(), PlanOptions{SnapshotID: "abc123", Target: TargetFiles})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	result, err := x.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.FilesRestored != 3 || result.BytesRestored != 100 {
		t.Errorf("result = %+v, want FilesRestored=3 BytesRestored=100", result)
	}
	if _, err := os.Stat(plan.Destination); err != nil {
		t.Errorf("staging directory %q does not exist after Execute: %v", plan.Destination, err)
	}

	events := sink.Events()
	if !hasEventType(events, event.TypeRestoreCompleted) {
		t.Errorf("expected a restore.completed event, got %v", eventTypes(events))
	}
}

func TestExecutor_Execute_Files_DestinationAlreadyExists_Revalidation(t *testing.T) {
	cfg := testExecutorConfig(t)
	fr := &fakeRestic{listFiles: []restic.FileInfo{{Path: "file1", Type: "file", Size: 10}}}
	x, _ := newTestExecutor(t, fr, nil, cfg)
	planner, _ := NewPlanner(fr, cfg)

	plan, err := planner.Plan(context.Background(), PlanOptions{SnapshotID: "abc", Target: TargetFiles})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// Simulate the destination having appeared between Plan and Execute.
	if err := os.MkdirAll(plan.Destination, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err = x.Execute(context.Background(), plan)
	var stale *ErrPlanStale
	if !errors.As(err, &stale) {
		t.Fatalf("Execute error = %v, want *ErrPlanStale", err)
	}
	if fr.restoreCallCount() != 0 {
		t.Error("restic.Restore must not be called when revalidation fails")
	}
}

func TestExecutor_Execute_TempDB_Success(t *testing.T) {
	cfg := testExecutorConfig(t)
	fr := &fakeRestic{
		listFiles:          []restic.FileInfo{{Path: filepath.Join(cfg.Backup.Root, "postgresql", "app.dump.zst"), Type: "file", Size: 555}},
		writeFileOnRestore: true,
	}
	fp := newFakePostgres()
	x, sink := newTestExecutor(t, fr, fp, cfg)
	planner, _ := NewPlanner(fr, cfg)

	plan, err := planner.Plan(context.Background(), PlanOptions{SnapshotID: "abc123", Target: TargetTempDB})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	result, err := x.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Destination != plan.TempDatabaseName {
		t.Errorf("result.Destination = %q, want %q", result.Destination, plan.TempDatabaseName)
	}
	if fp.createCallCount() != 1 {
		t.Errorf("CreateDatabase called %d times, want 1", fp.createCallCount())
	}
	if fp.dropCallCount() != 0 {
		t.Errorf("DropDatabase called %d times, want 0 on success", fp.dropCallCount())
	}
	if len(fp.restoreCalls) != 1 || fp.restoreCalls[0].Database != plan.TempDatabaseName {
		t.Errorf("RestoreToTemp calls = %+v, want exactly one targeting %q", fp.restoreCalls, plan.TempDatabaseName)
	}
	// The internal extraction directory is cleaned up after a successful
	// restore -- it isn't the deliverable, unlike the files-target
	// staging directory.
	if _, err := os.Stat(plan.Destination); !os.IsNotExist(err) {
		t.Errorf("extraction directory %q should have been removed after success, stat err = %v", plan.Destination, err)
	}

	events := sink.Events()
	if !hasEventType(events, event.TypeRestoreCompleted) {
		t.Errorf("expected a restore.completed event, got %v", eventTypes(events))
	}
}

func TestExecutor_Execute_TempDB_DatabaseAlreadyExists_Revalidation(t *testing.T) {
	cfg := testExecutorConfig(t)
	fr := &fakeRestic{listFiles: []restic.FileInfo{{Path: filepath.Join(cfg.Backup.Root, "postgresql", "app.dump.zst"), Type: "file", Size: 10}}}
	fp := newFakePostgres()
	x, _ := newTestExecutor(t, fr, fp, cfg)
	planner, _ := NewPlanner(fr, cfg)

	plan, err := planner.Plan(context.Background(), PlanOptions{SnapshotID: "abc", Target: TargetTempDB})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// Simulate the temp database name having been taken between Plan and
	// Execute (e.g. by a concurrent, unrelated process -- astronomically
	// unlikely given the random suffix, but revalidation must still
	// catch it deterministically in a test).
	fp.existing[plan.TempDatabaseName] = true

	_, err = x.Execute(context.Background(), plan)
	var stale *ErrPlanStale
	if !errors.As(err, &stale) {
		t.Fatalf("Execute error = %v, want *ErrPlanStale", err)
	}
	if fp.createCallCount() != 0 {
		t.Error("CreateDatabase must not be called when revalidation fails")
	}
}

func TestExecutor_Execute_TempDB_RestoreFailure_DropsOwnedDatabase(t *testing.T) {
	cfg := testExecutorConfig(t)
	fr := &fakeRestic{
		listFiles:          []restic.FileInfo{{Path: filepath.Join(cfg.Backup.Root, "postgresql", "app.dump.zst"), Type: "file", Size: 10}},
		writeFileOnRestore: true,
	}
	fp := newFakePostgres()
	fp.restoreErr = fmt.Errorf("pg_restore: simulated failure")
	x, sink := newTestExecutor(t, fr, fp, cfg)
	planner, _ := NewPlanner(fr, cfg)

	plan, err := planner.Plan(context.Background(), PlanOptions{SnapshotID: "abc", Target: TargetTempDB})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	_, err = x.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("Execute should fail when RestoreToTemp fails")
	}
	if fp.createCallCount() != 1 {
		t.Errorf("CreateDatabase called %d times, want 1 (it must have been created before the restore step could fail)", fp.createCallCount())
	}
	if fp.dropCallCount() != 1 || fp.dropCalls[0] != plan.TempDatabaseName {
		t.Errorf("DropDatabase calls = %v, want exactly [%q] -- the database this call created must be cleaned up", fp.dropCalls, plan.TempDatabaseName)
	}
	if fp.existing[plan.TempDatabaseName] {
		t.Error("temp database should no longer exist in the fake after cleanup")
	}

	events := sink.Events()
	if !hasEventType(events, event.TypeJobFailed) {
		t.Errorf("expected a job.failed event, got %v", eventTypes(events))
	}
}

func TestExecutor_Execute_TempDB_CreateDatabaseFailure_NeverDrops(t *testing.T) {
	cfg := testExecutorConfig(t)
	fr := &fakeRestic{listFiles: []restic.FileInfo{{Path: filepath.Join(cfg.Backup.Root, "postgresql", "app.dump.zst"), Type: "file", Size: 10}}, writeFileOnRestore: true}
	fp := newFakePostgres()
	fp.createErr = fmt.Errorf("createdb: simulated failure")
	x, _ := newTestExecutor(t, fr, fp, cfg)
	planner, _ := NewPlanner(fr, cfg)

	plan, err := planner.Plan(context.Background(), PlanOptions{SnapshotID: "abc", Target: TargetTempDB})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	_, err = x.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("Execute should fail when CreateDatabase fails")
	}
	if fp.dropCallCount() != 0 {
		t.Errorf("DropDatabase called %d times, want 0 -- the database was never successfully created by this call, so there is nothing owned to clean up", fp.dropCallCount())
	}
}

func TestExecutor_Execute_BackupInProgress_Refuses(t *testing.T) {
	cfg := testExecutorConfig(t)
	fr := &fakeRestic{listFiles: []restic.FileInfo{{Path: "file1", Type: "file", Size: 10}}}
	x, _ := newTestExecutor(t, fr, nil, cfg)
	planner, _ := NewPlanner(fr, cfg)

	plan, err := planner.Plan(context.Background(), PlanOptions{SnapshotID: "abc", Target: TargetFiles})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	backupLock, ok, err := lock.TryAcquire(cfg.Backup.LockFile)
	if err != nil || !ok {
		t.Fatalf("setup: acquire backup lock: ok=%v err=%v", ok, err)
	}
	defer backupLock.Release()

	_, err = x.Execute(context.Background(), plan)
	if !errors.Is(err, ErrBackupInProgress) {
		t.Errorf("Execute error = %v, want ErrBackupInProgress", err)
	}
	if fr.restoreCallCount() != 0 {
		t.Error("restic.Restore must not be called when a backup is in progress")
	}
}

func TestExecutor_Execute_ConcurrentRestoresAreSerialized(t *testing.T) {
	cfg := testExecutorConfig(t)
	fr := &fakeRestic{listFiles: []restic.FileInfo{{Path: "file1", Type: "file", Size: 10}}}
	x, _ := newTestExecutor(t, fr, nil, cfg)
	planner, _ := NewPlanner(fr, cfg)

	plan, err := planner.Plan(context.Background(), PlanOptions{SnapshotID: "abc", Target: TargetFiles})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	restoreLock, ok, err := lock.TryAcquire(cfg.Restore.LockFile)
	if err != nil || !ok {
		t.Fatalf("setup: acquire restore lock: ok=%v err=%v", ok, err)
	}
	defer restoreLock.Release()

	_, err = x.Execute(context.Background(), plan)
	if !errors.Is(err, lock.ErrLocked) {
		t.Errorf("Execute error = %v, want lock.ErrLocked", err)
	}
}

func TestExecutor_Execute_JobHistoryRecorded(t *testing.T) {
	cfg := testExecutorConfig(t)
	fr := &fakeRestic{listFiles: []restic.FileInfo{{Path: "file1", Type: "file", Size: 25}, {Path: "file2", Type: "file", Size: 25}}, restoreSummary: restic.RestoreSummary{FilesRestored: 2, BytesRestored: 50}, writeFileOnRestore: true}
	sink := &event.InMemorySink{}
	jobs := newTestJobStore(t)
	x, err := NewExecutor(fr, nil, cfg, jobs, sink, nil)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	planner, _ := NewPlanner(fr, cfg)
	plan, err := planner.Plan(context.Background(), PlanOptions{SnapshotID: "abc", Target: TargetFiles})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	result, err := x.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	recorded, err := jobs.Get(context.Background(), result.JobID)
	if err != nil {
		t.Fatalf("jobs.Get: %v", err)
	}
	if recorded.State != job.StateCompleted {
		t.Errorf("job state = %s, want %s", recorded.State, job.StateCompleted)
	}
	if recorded.Type != job.TypeRestore {
		t.Errorf("job type = %s, want %s", recorded.Type, job.TypeRestore)
	}
	if recorded.Metadata.SnapshotID != plan.SnapshotID {
		t.Errorf("job metadata SnapshotID = %q, want %q", recorded.Metadata.SnapshotID, plan.SnapshotID)
	}
}

func TestExecutor_Execute_CancellationDuringRestoreMarksJobCancelled(t *testing.T) {
	cfg := testExecutorConfig(t)
	fr := &fakeRestic{listFiles: []restic.FileInfo{{Path: "file1", Type: "file", Size: 10}}, restoreErr: fmt.Errorf("restic restore: %w", context.Canceled)}
	sink := &event.InMemorySink{}
	jobs := newTestJobStore(t)
	x, err := NewExecutor(fr, nil, cfg, jobs, sink, nil)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	planner, _ := NewPlanner(fr, cfg)
	plan, err := planner.Plan(context.Background(), PlanOptions{SnapshotID: "abc", Target: TargetFiles})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	result, err := x.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("Execute should fail when the restic call is cancelled")
	}

	recorded, getErr := jobs.Get(context.Background(), result.JobID)
	if getErr != nil {
		t.Fatalf("jobs.Get: %v", getErr)
	}
	if recorded.State != job.StateCancelled {
		t.Errorf("job state = %s, want %s (cancellation must be distinguished from a plain failure)", recorded.State, job.StateCancelled)
	}
	if recorded.ErrorCategory != job.ErrorCategoryCancelled {
		t.Errorf("job error category = %s, want %s", recorded.ErrorCategory, job.ErrorCategoryCancelled)
	}

	events := sink.Events()
	if !hasEventType(events, event.TypeJobCancelled) {
		t.Errorf("expected a job.cancelled event, got %v", eventTypes(events))
	}
	if hasEventType(events, event.TypeJobFailed) {
		t.Error("a cancelled job should not also emit job.failed")
	}
}

func TestExecutor_Execute_FilesFailure_MarksStagingIncomplete(t *testing.T) {
	cfg := testExecutorConfig(t)
	fr := &fakeRestic{listFiles: []restic.FileInfo{{Path: "file1", Type: "file", Size: 10}}, restoreErr: fmt.Errorf("restic restore: simulated failure")}
	x, _ := newTestExecutor(t, fr, nil, cfg)
	planner, _ := NewPlanner(fr, cfg)
	plan, err := planner.Plan(context.Background(), PlanOptions{SnapshotID: "abc", Target: TargetFiles})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	_, err = x.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("Execute should fail")
	}
	marker := filepath.Join(plan.Destination, ".incomplete")
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Errorf("expected an .incomplete marker file at %q: %v", marker, statErr)
	}
}

func TestNewExecutor_RequiresJobStore(t *testing.T) {
	cfg := testExecutorConfig(t)
	if _, err := NewExecutor(&fakeRestic{}, nil, cfg, nil, nil, nil); err == nil {
		t.Fatal("NewExecutor with a nil job store should fail -- every restore must appear in job history")
	}
}

func hasEventType(events []event.Event, typ event.Type) bool {
	for _, e := range events {
		if e.Type == typ {
			return true
		}
	}
	return false
}

func eventTypes(events []event.Event) []event.Type {
	out := make([]event.Type, len(events))
	for i, e := range events {
		out[i] = e.Type
	}
	return out
}
