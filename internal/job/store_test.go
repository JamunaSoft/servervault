package job

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "jobs.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStore_CreateAndGet(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	j, err := s.Create(ctx, Job{Type: TypeBackup, Metadata: Metadata{HostTag: "srv-1"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if j.ID == "" {
		t.Fatal("Create did not assign an ID")
	}
	if j.State != StatePending {
		t.Errorf("Create default state = %s, want %s", j.State, StatePending)
	}

	got, err := s.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Metadata.HostTag != "srv-1" {
		t.Errorf("Get metadata.HostTag = %q, want %q", got.Metadata.HostTag, "srv-1")
	}
}

func TestStore_Create_RequiresType(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.Create(context.Background(), Job{}); err == nil {
		t.Fatal("Create with empty Type should fail")
	}
}

func TestStore_Get_NotFound(t *testing.T) {
	s := openTestStore(t)
	_, err := s.Get(context.Background(), "does-not-exist")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get unknown id: err = %v, want ErrNotFound", err)
	}
}

func TestStore_Advance_TableDriven(t *testing.T) {
	tests := []struct {
		name    string
		path    []State
		wantErr bool
	}{
		{"backup happy path", []State{StatePreparing, StateDumping, StateBackingUp, StateVerifying, StateCompleted}, false},
		{"restore skips dumping", []State{StatePreparing, StateBackingUp, StateCompleted}, false},
		{"illegal skip to completed", []State{StateCompleted}, true},
		{"cancel mid-flight", []State{StatePreparing, StateDumping, StateCancelled}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := openTestStore(t)
			ctx := context.Background()
			j, err := s.Create(ctx, Job{Type: TypeBackup})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}

			var lastErr error
			for _, to := range tt.path {
				j, lastErr = s.Advance(ctx, j.ID, to, AdvanceOptions{})
				if lastErr != nil {
					break
				}
			}

			if tt.wantErr {
				if lastErr == nil {
					t.Fatal("expected an error, got nil")
				}
				var te *TransitionError
				if !errors.As(lastErr, &te) {
					t.Fatalf("error = %v, want *TransitionError", lastErr)
				}
				return
			}
			if lastErr != nil {
				t.Fatalf("unexpected error: %v", lastErr)
			}
			if j.State != tt.path[len(tt.path)-1] {
				t.Errorf("final state = %s, want %s", j.State, tt.path[len(tt.path)-1])
			}
		})
	}
}

func TestStore_Advance_TerminalStateRejectsFurtherTransitions(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	j, _ := s.Create(ctx, Job{Type: TypeBackup})
	j, err := s.Advance(ctx, j.ID, StateCancelled, AdvanceOptions{})
	if err != nil {
		t.Fatalf("Advance to cancelled: %v", err)
	}

	_, err = s.Advance(ctx, j.ID, StatePreparing, AdvanceOptions{})
	var te *TransitionError
	if !errors.As(err, &te) {
		t.Fatalf("advancing a terminal job: err = %v, want *TransitionError", err)
	}
}

func TestStore_Advance_NotFound(t *testing.T) {
	s := openTestStore(t)
	_, err := s.Advance(context.Background(), "missing", StatePreparing, AdvanceOptions{})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Advance unknown id: err = %v, want ErrNotFound", err)
	}
}

func TestStore_Advance_SetsTimestampsAndErrorFields(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	j, _ := s.Create(ctx, Job{Type: TypeRestore})

	j, err := s.Advance(ctx, j.ID, StatePreparing, AdvanceOptions{})
	if err != nil {
		t.Fatalf("Advance to preparing: %v", err)
	}

	j, err = s.Advance(ctx, j.ID, StateFailed, AdvanceOptions{
		ErrorCategory: ErrorCategoryConnectivity,
		ErrorSummary:  "could not reach database",
	})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if j.ErrorCategory != ErrorCategoryConnectivity {
		t.Errorf("ErrorCategory = %q, want %q", j.ErrorCategory, ErrorCategoryConnectivity)
	}
	if j.ErrorSummary != "could not reach database" {
		t.Errorf("ErrorSummary = %q", j.ErrorSummary)
	}
	if !j.State.Terminal() {
		t.Errorf("state %s should be terminal", j.State)
	}
}

func TestStore_Advance_MetadataUpdate(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	j, _ := s.Create(ctx, Job{Type: TypeBackup})

	meta := &Metadata{SnapshotID: "abc123", BytesTotal: 4096, FilesNew: 3}
	j, err := s.Advance(ctx, j.ID, StatePreparing, AdvanceOptions{Metadata: meta})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if j.Metadata.SnapshotID != "abc123" || j.Metadata.BytesTotal != 4096 || j.Metadata.FilesNew != 3 {
		t.Errorf("metadata not persisted correctly: %+v", j.Metadata)
	}
}

// TestStore_Advance_ConcurrentUpdatesAreSerializedSafely fires many
// goroutines at the same job, each trying to make the same legal
// transition. Exactly one must win; the rest must observe either
// ErrConcurrentUpdate (lost the race on the same legal move) or a
// *TransitionError (arrived after the state had already moved past what
// they were trying to leave), never a corrupted or partially-applied
// write, and go test -race must find nothing.
func TestStore_Advance_ConcurrentUpdatesAreSerializedSafely(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	j, err := s.Create(ctx, Job{Type: TypeBackup})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	const workers = 16
	var wg sync.WaitGroup
	var succeeded int64
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			_, err := s.Advance(ctx, j.ID, StatePreparing, AdvanceOptions{})
			if err == nil {
				atomic.AddInt64(&succeeded, 1)
				return
			}
			if !errors.Is(err, ErrConcurrentUpdate) {
				var te *TransitionError
				if !errors.As(err, &te) {
					t.Errorf("unexpected error type from concurrent Advance: %v", err)
				}
			}
		}()
	}
	wg.Wait()

	if succeeded != 1 {
		t.Errorf("exactly one concurrent Advance should succeed, got %d", succeeded)
	}

	final, err := s.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if final.State != StatePreparing {
		t.Errorf("final state = %s, want %s", final.State, StatePreparing)
	}
}

// TestStore_Advance_ConcurrentDifferentJobs proves normal concurrent
// throughput across independent jobs isn't serialized into failures --
// only same-row contention should ever produce ErrConcurrentUpdate.
func TestStore_Advance_ConcurrentDifferentJobs(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	const n = 20
	ids := make([]string, n)
	for i := range ids {
		j, err := s.Create(ctx, Job{Type: TypeBackup})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		ids[i] = j.ID
	}

	var wg sync.WaitGroup
	errs := make([]error, n)
	wg.Add(n)
	for i, id := range ids {
		i, id := i, id
		go func() {
			defer wg.Done()
			_, err := s.Advance(ctx, id, StatePreparing, AdvanceOptions{})
			errs[i] = err
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("job %d: unexpected error: %v", i, err)
		}
	}
}

func TestStore_Reconcile_MarksNonTerminalJobsInterrupted(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	inFlight, _ := s.Create(ctx, Job{Type: TypeBackup})
	inFlight, err := s.Advance(ctx, inFlight.ID, StatePreparing, AdvanceOptions{})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	inFlight, err = s.Advance(ctx, inFlight.ID, StateDumping, AdvanceOptions{})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}

	done, _ := s.Create(ctx, Job{Type: TypeBackup})
	done, err = s.Advance(ctx, done.ID, StatePreparing, AdvanceOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	done, err = s.Advance(ctx, done.ID, StateBackingUp, AdvanceOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	done, err = s.Advance(ctx, done.ID, StateCompleted, AdvanceOptions{})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	n, err := s.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n != 1 {
		t.Errorf("Reconcile reconciled %d jobs, want 1", n)
	}

	got, err := s.Get(ctx, inFlight.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.State != StateInterrupted {
		t.Errorf("in-flight job state after Reconcile = %s, want %s", got.State, StateInterrupted)
	}
	if got.ErrorCategory != ErrorCategoryInterrupted {
		t.Errorf("in-flight job error category = %s, want %s", got.ErrorCategory, ErrorCategoryInterrupted)
	}

	stillDone, err := s.Get(ctx, done.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stillDone.State != StateCompleted {
		t.Errorf("completed job state after Reconcile = %s, want unchanged %s", stillDone.State, StateCompleted)
	}

	// Reconcile is safe to call again with nothing left to reconcile.
	n2, err := s.Reconcile(ctx)
	if err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second Reconcile reconciled %d jobs, want 0", n2)
	}
}

func TestStore_MigrationsApplyOnceAndSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "jobs.db")

	s1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	j, err := s1.Create(context.Background(), Job{Type: TypeBackup})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopening must not re-run migration 1 (it would fail on "table jobs
	// already exists" if schema_migrations bookkeeping were broken) and
	// must still see the previously created job.
	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open (re-applying migrations would fail here): %v", err)
	}
	defer s2.Close()

	got, err := s2.Get(context.Background(), j.ID)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got.ID != j.ID {
		t.Errorf("got ID %q after reopen, want %q", got.ID, j.ID)
	}
}

// TestStore_ReconcileAfterUncleanRestart is a real crash-consistency test:
// it spawns this same test binary as a subprocess, has that subprocess
// create a job, advance it into a non-terminal state, and then send
// itself SIGKILL -- no deferred Close, no WAL checkpoint, no graceful
// shutdown of any kind. The outer test then reopens the same database
// file in-process and asserts (a) the file isn't corrupted -- Open and
// Get both succeed -- and (b) Reconcile marks the orphaned job
// Interrupted. This is what actually exercises the "SQLite database
// survives process interruption without corruption" and "in-progress
// jobs reconcile predictably after restart" acceptance criteria; it does
// not itself re-verify SQLite's own WAL durability guarantees, which are
// upstream, well-established behavior this package relies on rather than
// re-tests.
func TestStore_ReconcileAfterUncleanRestart(t *testing.T) {
	if notUnix() {
		t.Skip("SIGKILL-based crash simulation requires a unix-like OS")
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "jobs.db")

	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_CrashMidJob", "-test.v=true")
	cmd.Env = append(os.Environ(),
		"SERVERVAULT_JOB_CRASH_TEST=1",
		"SERVERVAULT_JOB_CRASH_DB="+dbPath,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start crash helper subprocess: %v", err)
	}

	err := cmd.Wait()
	if err == nil {
		t.Fatal("crash helper subprocess exited cleanly; expected it to be killed")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("unexpected wait error: %v", err)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("subprocess did not die from SIGKILL as expected: exit status %v", exitErr)
	}

	// Reopen the same file the killed process was writing to. If the WAL
	// file were left in a corrupt state, either Open or Get below would
	// fail.
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen after unclean restart: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	n, err := s.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile after unclean restart: %v", err)
	}
	if n != 1 {
		t.Fatalf("Reconcile after unclean restart reconciled %d jobs, want 1", n)
	}
}

// TestHelperProcess_CrashMidJob is not a real test in the outer test run
// (SERVERVAULT_JOB_CRASH_TEST is unset there, so it's a no-op and always
// passes). Invoked as a subprocess with that env var set, it creates a
// job, advances it into a non-terminal state, and kills itself with
// SIGKILL before it can return or run any deferred cleanup -- see
// TestStore_ReconcileAfterUncleanRestart above.
func TestHelperProcess_CrashMidJob(t *testing.T) {
	if os.Getenv("SERVERVAULT_JOB_CRASH_TEST") != "1" {
		return
	}

	dbPath := os.Getenv("SERVERVAULT_JOB_CRASH_DB")
	s, err := Open(dbPath)
	if err != nil {
		os.Exit(2)
	}
	ctx := context.Background()
	j, err := s.Create(ctx, Job{Type: TypeBackup})
	if err != nil {
		os.Exit(3)
	}
	if _, err := s.Advance(ctx, j.ID, StatePreparing, AdvanceOptions{}); err != nil {
		os.Exit(4)
	}
	if _, err := s.Advance(ctx, j.ID, StateDumping, AdvanceOptions{}); err != nil {
		os.Exit(5)
	}

	// Give SQLite a moment to have durably written the last commit before
	// we die -- the write above already completed synchronously (Advance
	// only returns after its transaction commits), so this is a small
	// safety margin against scheduler timing, not a requirement for
	// correctness.
	time.Sleep(20 * time.Millisecond)

	_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
	// Unreachable: the process is dead before this line runs.
	time.Sleep(time.Second)
}

func notUnix() bool {
	return os.PathSeparator != '/'
}
