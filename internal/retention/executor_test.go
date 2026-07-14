package retention

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/event"
	"github.com/JamunaSoft/servervault/internal/job"
	"github.com/JamunaSoft/servervault/internal/lock"
	"github.com/JamunaSoft/servervault/internal/restic"
)

func testExecutorConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	cfg := testRetentionConfig()
	cfg.Backup.LockFile = filepath.Join(dir, "backup.lock")
	cfg.Restore.LockFile = filepath.Join(dir, "restore.lock")
	cfg.Retention.LockFile = filepath.Join(dir, "prune.lock")
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

func newTestExecutor(t *testing.T, fr *fakeRestic, cfg *config.Config) (*Executor, *event.InMemorySink) {
	t.Helper()
	sink := &event.InMemorySink{}
	x, err := NewExecutor(fr, cfg, newTestJobStore(t), sink, nil)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	return x, sink
}

// happyPathRestic returns a fakeRestic whose dry-run and real forget
// calls agree with each other -- the common case, where nothing changes
// between planning and execution.
func happyPathRestic() *fakeRestic {
	return &fakeRestic{
		snapshots: snapshotIDs(5),
		forgetSummary: restic.ForgetSummary{
			KeptSnapshotIDs:    []string{"k1", "k2", "k3"},
			RemovedSnapshotIDs: []string{"r1", "r2"},
		},
	}
}

func TestExecutor_Execute_Success(t *testing.T) {
	cfg := testExecutorConfig(t)
	fr := happyPathRestic()
	planner, err := NewPlanner(fr, cfg)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	plan, err := planner.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	x, sink := newTestExecutor(t, fr, cfg)
	result, err := x.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.RemovedCount != 2 {
		t.Errorf("RemovedCount = %d, want 2", result.RemovedCount)
	}
	if len(result.RemovedSnapshotIDs) != 2 {
		t.Errorf("RemovedSnapshotIDs = %v, want 2 entries", result.RemovedSnapshotIDs)
	}
	if result.JobID == "" {
		t.Error("JobID must not be empty")
	}

	// Exactly one real (Prune=true) forget call, after the two dry-run
	// calls (Plan's own, then Execute's revalidation).
	if fr.forgetCallCount() != 3 {
		t.Fatalf("forget called %d times, want 3 (plan dry-run, revalidation dry-run, real prune)", fr.forgetCallCount())
	}
	last := fr.lastForgetCall()
	if !last.Prune {
		t.Error("the final forget call must set Prune")
	}
	if last.DryRun {
		t.Error("the final forget call must not set DryRun")
	}

	events := sink.Events()
	if !hasEventType(events, event.TypeRetentionCompleted) {
		t.Errorf("expected a retention.completed event, got %v", eventTypes(events))
	}
}

func TestExecutor_Execute_LockConflict(t *testing.T) {
	cfg := testExecutorConfig(t)
	fr := happyPathRestic()
	planner, _ := NewPlanner(fr, cfg)
	plan, err := planner.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	pruneLock, ok, err := lock.TryAcquire(cfg.Retention.LockFile)
	if err != nil || !ok {
		t.Fatalf("setup: acquire prune lock: ok=%v err=%v", ok, err)
	}
	defer pruneLock.Release()

	fr2 := happyPathRestic() // separate instance so we can assert no further forget calls happened on it
	x, _ := newTestExecutor(t, fr2, cfg)

	_, err = x.Execute(context.Background(), plan)
	if !errors.Is(err, lock.ErrLocked) {
		t.Errorf("Execute error = %v, want lock.ErrLocked", err)
	}
	if fr2.forgetCallCount() != 0 {
		t.Error("Forget must not be called when the prune lock is already held")
	}
}

func TestExecutor_Execute_BackupInProgress_Refuses(t *testing.T) {
	cfg := testExecutorConfig(t)
	fr := happyPathRestic()
	planner, _ := NewPlanner(fr, cfg)
	plan, err := planner.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	backupLock, ok, err := lock.TryAcquire(cfg.Backup.LockFile)
	if err != nil || !ok {
		t.Fatalf("setup: acquire backup lock: ok=%v err=%v", ok, err)
	}
	defer backupLock.Release()

	x, _ := newTestExecutor(t, fr, cfg)
	_, err = x.Execute(context.Background(), plan)
	if !errors.Is(err, ErrBackupInProgress) {
		t.Errorf("Execute error = %v, want ErrBackupInProgress", err)
	}
	// Only Plan's own dry-run call should have happened -- Execute must
	// refuse before making any further restic call.
	if fr.forgetCallCount() != 1 {
		t.Errorf("forget called %d times after refusing, want 1 (only Plan's own)", fr.forgetCallCount())
	}
}

func TestExecutor_Execute_RestoreInProgress_Refuses(t *testing.T) {
	cfg := testExecutorConfig(t)
	fr := happyPathRestic()
	planner, _ := NewPlanner(fr, cfg)
	plan, err := planner.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	restoreLock, ok, err := lock.TryAcquire(cfg.Restore.LockFile)
	if err != nil || !ok {
		t.Fatalf("setup: acquire restore lock: ok=%v err=%v", ok, err)
	}
	defer restoreLock.Release()

	x, _ := newTestExecutor(t, fr, cfg)
	_, err = x.Execute(context.Background(), plan)
	if !errors.Is(err, ErrRestoreInProgress) {
		t.Errorf("Execute error = %v, want ErrRestoreInProgress", err)
	}
}

func TestExecutor_Execute_PlanStale_RemovalSetChanged(t *testing.T) {
	cfg := testExecutorConfig(t)
	fr := &fakeRestic{
		snapshots: snapshotIDs(5),
		forgetSequence: []restic.ForgetSummary{
			{RemovedSnapshotIDs: []string{"r1", "r2"}},       // Plan's own dry run
			{RemovedSnapshotIDs: []string{"r1", "r2", "r3"}}, // revalidation: a new snapshot appeared, changing the removal set
		},
	}
	planner, _ := NewPlanner(fr, cfg)
	plan, err := planner.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.RemoveCount != 2 {
		t.Fatalf("setup: plan.RemoveCount = %d, want 2", plan.RemoveCount)
	}

	x, sink := newTestExecutor(t, fr, cfg)
	_, err = x.Execute(context.Background(), plan)
	var stale *ErrPlanStale
	if !errors.As(err, &stale) {
		t.Fatalf("Execute error = %v, want *ErrPlanStale", err)
	}

	// The real, destructive call must never have happened: exactly 2
	// forget calls (Plan's, then the revalidation), both dry runs.
	if fr.forgetCallCount() != 2 {
		t.Fatalf("forget called %d times, want 2 -- the real prune must never run when the plan is stale", fr.forgetCallCount())
	}
	for _, call := range fr.forgetCalls {
		if call.Prune {
			t.Error("no forget call should have Prune set when revalidation detects drift")
		}
	}

	events := sink.Events()
	if !hasEventType(events, event.TypeJobFailed) {
		t.Errorf("expected a job.failed event, got %v", eventTypes(events))
	}
}

func TestExecutor_Execute_ForgetFailure_MarksJobFailed(t *testing.T) {
	cfg := testExecutorConfig(t)
	fr := happyPathRestic()
	planner, _ := NewPlanner(fr, cfg)
	plan, err := planner.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// Fail only the real (non-dry-run) forget call -- let Plan and
	// revalidation succeed so the failure is attributable specifically
	// to the destructive step.
	failing := &sequencedForgetFailure{fakeRestic: fr, failFrom: 3}
	x, err := NewExecutor(failing, cfg, newTestJobStore(t), &event.InMemorySink{}, nil)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	result, err := x.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("Execute should fail when the real forget call fails")
	}

	recorded, getErr := newTestJobStoreGet(t, x, result.JobID)
	if getErr != nil {
		t.Fatalf("jobs.Get: %v", getErr)
	}
	if recorded.State != job.StateFailed {
		t.Errorf("job state = %s, want %s", recorded.State, job.StateFailed)
	}
}

func TestExecutor_Execute_CancellationDuringForgetMarksJobCancelled(t *testing.T) {
	cfg := testExecutorConfig(t)
	fr := happyPathRestic()
	planner, _ := NewPlanner(fr, cfg)
	plan, err := planner.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	failing := &sequencedForgetFailure{fakeRestic: fr, failFrom: 3, err: fmt.Errorf("restic forget: %w", context.Canceled)}
	jobs := newTestJobStore(t)
	sink := &event.InMemorySink{}
	x, err := NewExecutor(failing, cfg, jobs, sink, nil)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	result, err := x.Execute(context.Background(), plan)
	if err == nil {
		t.Fatal("Execute should fail when the forget call is cancelled")
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

func TestExecutor_Execute_JobHistoryRecorded(t *testing.T) {
	cfg := testExecutorConfig(t)
	fr := happyPathRestic()
	planner, _ := NewPlanner(fr, cfg)
	plan, err := planner.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	jobs := newTestJobStore(t)
	x, err := NewExecutor(fr, cfg, jobs, &event.InMemorySink{}, nil)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
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
	if recorded.Type != job.TypePrune {
		t.Errorf("job type = %s, want %s", recorded.Type, job.TypePrune)
	}
	if recorded.Metadata.SnapshotsRemoved != 2 {
		t.Errorf("job metadata SnapshotsRemoved = %d, want 2", recorded.Metadata.SnapshotsRemoved)
	}
}

func TestNewExecutor_RequiresNonNilArgs(t *testing.T) {
	cfg := testExecutorConfig(t)
	if _, err := NewExecutor(nil, cfg, newTestJobStore(t), nil, nil); err == nil {
		t.Error("NewExecutor with a nil restic client should fail")
	}
	if _, err := NewExecutor(&fakeRestic{}, nil, newTestJobStore(t), nil, nil); err == nil {
		t.Error("NewExecutor with a nil config should fail")
	}
	if _, err := NewExecutor(&fakeRestic{}, cfg, nil, nil, nil); err == nil {
		t.Error("NewExecutor with a nil job store should fail -- every prune must appear in job history")
	}
}

// sequencedForgetFailure wraps a fakeRestic so only forget calls at or
// after failFrom (1-indexed) fail -- used to fail specifically the real,
// destructive call while letting Plan's and revalidation's dry runs
// succeed normally.
type sequencedForgetFailure struct {
	*fakeRestic
	failFrom int
	err      error
}

func (f *sequencedForgetFailure) Forget(ctx context.Context, opts restic.ForgetOptions) (restic.ForgetSummary, error) {
	if f.forgetCallCount()+1 >= f.failFrom {
		f.fakeRestic.mu.Lock()
		f.fakeRestic.forgetCalls = append(f.fakeRestic.forgetCalls, opts)
		f.fakeRestic.mu.Unlock()
		if f.err != nil {
			return restic.ForgetSummary{}, f.err
		}
		return restic.ForgetSummary{}, errors.New("simulated forget failure")
	}
	return f.fakeRestic.Forget(ctx, opts)
}

func newTestJobStoreGet(t *testing.T, x *Executor, id string) (job.Job, error) {
	t.Helper()
	return x.jobs.Get(context.Background(), id)
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
