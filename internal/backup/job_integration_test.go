package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/event"
	"github.com/JamunaSoft/servervault/internal/job"
	"github.com/JamunaSoft/servervault/internal/lock"
	"github.com/JamunaSoft/servervault/internal/postgres"
	"github.com/JamunaSoft/servervault/internal/restic"
)

func newTestJobStore(t *testing.T) *job.Store {
	t.Helper()
	s, err := job.Open(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatalf("job.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// testEngineWithTracking builds an Engine the same way testEngine does
// (direct struct construction, so fakeDumper/fakeBacker can be injected
// without a real subprocess runner) but with a real, temporary
// job.Store and an event.InMemorySink attached, so tests can assert on
// both.
func testEngineWithTracking(t *testing.T, cfg *config.Config, dumper Dumper, backer Backer) (*Engine, *job.Store, *event.InMemorySink) {
	t.Helper()
	jobs := newTestJobStore(t)
	sink := &event.InMemorySink{}
	e := &Engine{
		cfg:      cfg,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		postgres: dumper,
		restic:   backer,
		jobs:     jobs,
		events:   sink,
	}
	return e, jobs, sink
}

func eventTypesOf(events []event.Event) []event.Type {
	out := make([]event.Type, len(events))
	for i, e := range events {
		out[i] = e.Type
	}
	return out
}

func hasEventType(events []event.Event, typ event.Type) bool {
	for _, e := range events {
		if e.Type == typ {
			return true
		}
	}
	return false
}

// TestEngine_Run_JobLifecycleIntegration is the table-driven suite this
// milestone's completion pass asks for: success, dump failure,
// verification failure, restic failure, cancellation, and lock-busy
// behavior, each checked against real internal/job state and real
// internal/event emissions -- not just Engine.Run's returned error, the
// way the pre-existing tests in backup_test.go already check.
func TestEngine_Run_JobLifecycleIntegration(t *testing.T) {
	tests := []struct {
		name string
		// build returns the Dumper/Backer fakes and any pre-Run setup
		// (e.g. writing a fixture dump file). cfg is the shared,
		// per-test base config (already has a valid, isolated
		// lock file/root under t.TempDir()).
		build func(t *testing.T, cfg *config.Config) (Dumper, Backer)
		// preAcquireLock simulates a concurrently running backup.
		preAcquireLock bool

		wantErr           bool
		wantJobState      job.State
		wantErrorCategory job.ErrorCategory
		wantEvents        []event.Type // must all be present
		wantAbsentEvents  []event.Type // must not be present
	}{
		{
			name: "success",
			build: func(t *testing.T, cfg *config.Config) (Dumper, Backer) {
				dumpPath := filepath.Join(t.TempDir(), "app.dump.zst")
				if err := os.WriteFile(dumpPath, []byte("dump"), 0o600); err != nil {
					t.Fatalf("setup: %v", err)
				}
				return &fakeDumper{dumpMeta: postgres.Metadata{Path: dumpPath, Bytes: 4}},
					&fakeBacker{summary: restic.Summary{SnapshotID: "abc123", FilesNew: 3}}
			},
			wantJobState: job.StateCompleted,
			wantEvents: []event.Type{
				event.TypeJobCreated, event.TypeJobStarted,
				event.TypeDatabaseDumpStarted, event.TypeDatabaseDumpCompleted,
				event.TypeVerificationStarted, event.TypeVerificationCompleted,
				event.TypeBackupStarted, event.TypeBackupCompleted,
			},
			wantAbsentEvents: []event.Type{event.TypeJobFailed, event.TypeJobCancelled},
		},
		{
			name: "dump failure",
			build: func(t *testing.T, cfg *config.Config) (Dumper, Backer) {
				return &fakeDumper{dumpErr: errors.New("pg_dump: connection reset")}, &fakeBacker{}
			},
			wantErr:           true,
			wantJobState:      job.StateFailed,
			wantErrorCategory: job.ErrorCategoryExecution,
			wantEvents:        []event.Type{event.TypeJobCreated, event.TypeJobStarted, event.TypeDatabaseDumpStarted, event.TypeJobFailed},
			wantAbsentEvents:  []event.Type{event.TypeDatabaseDumpCompleted, event.TypeBackupStarted, event.TypeJobCancelled},
		},
		{
			name: "verification failure",
			build: func(t *testing.T, cfg *config.Config) (Dumper, Backer) {
				dumpPath := filepath.Join(t.TempDir(), "corrupt.dump.zst")
				if err := os.WriteFile(dumpPath, []byte("corrupt"), 0o600); err != nil {
					t.Fatalf("setup: %v", err)
				}
				return &fakeDumper{dumpMeta: postgres.Metadata{Path: dumpPath}, verifyErr: errors.New("pg_restore: unexpected end of file")},
					&fakeBacker{}
			},
			wantErr:           true,
			wantJobState:      job.StateFailed,
			wantErrorCategory: job.ErrorCategoryVerification,
			wantEvents: []event.Type{
				event.TypeJobCreated, event.TypeJobStarted,
				event.TypeDatabaseDumpStarted, event.TypeDatabaseDumpCompleted,
				event.TypeVerificationStarted, event.TypeJobFailed,
			},
			wantAbsentEvents: []event.Type{event.TypeVerificationCompleted, event.TypeBackupStarted},
		},
		{
			name: "restic failure",
			build: func(t *testing.T, cfg *config.Config) (Dumper, Backer) {
				dumpPath := filepath.Join(t.TempDir(), "app.dump.zst")
				if err := os.WriteFile(dumpPath, []byte("dump"), 0o600); err != nil {
					t.Fatalf("setup: %v", err)
				}
				return &fakeDumper{dumpMeta: postgres.Metadata{Path: dumpPath}},
					&fakeBacker{err: errors.New("restic: repository lock failed")}
			},
			wantErr:           true,
			wantJobState:      job.StateFailed,
			wantErrorCategory: job.ErrorCategoryExecution,
			wantEvents: []event.Type{
				event.TypeJobCreated, event.TypeJobStarted,
				event.TypeDatabaseDumpCompleted, event.TypeVerificationCompleted,
				event.TypeBackupStarted, event.TypeJobFailed,
			},
			wantAbsentEvents: []event.Type{event.TypeBackupCompleted, event.TypeJobCancelled},
		},
		{
			name:           "lock busy",
			preAcquireLock: true,
			build: func(t *testing.T, cfg *config.Config) (Dumper, Backer) {
				return &fakeDumper{}, &fakeBacker{}
			},
			wantErr:           true,
			wantJobState:      job.StateFailed,
			wantErrorCategory: job.ErrorCategoryLock,
			// TypeJobStarted still fires: the job reaches "preparing"
			// (which now covers the lock attempt itself) before the
			// lock-busy failure is recorded -- see Run()'s comment on
			// why preparing is entered before the lock attempt.
			wantEvents:       []event.Type{event.TypeJobCreated, event.TypeJobStarted, event.TypeJobFailed},
			wantAbsentEvents: []event.Type{event.TypeJobCancelled, event.TypeDatabaseDumpStarted},
		},
		{
			name: "cancellation during restic backup",
			build: func(t *testing.T, cfg *config.Config) (Dumper, Backer) {
				dumpPath := filepath.Join(t.TempDir(), "app.dump.zst")
				if err := os.WriteFile(dumpPath, []byte("dump"), 0o600); err != nil {
					t.Fatalf("setup: %v", err)
				}
				return &fakeDumper{dumpMeta: postgres.Metadata{Path: dumpPath}},
					&fakeBacker{err: fmt.Errorf("restic backup: %w", context.Canceled)}
			},
			wantErr:           true,
			wantJobState:      job.StateCancelled,
			wantErrorCategory: job.ErrorCategoryCancelled,
			wantEvents:        []event.Type{event.TypeJobCreated, event.TypeBackupStarted, event.TypeJobCancelled},
			wantAbsentEvents:  []event.Type{event.TypeJobFailed, event.TypeBackupCompleted},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig(t)
			dumper, backer := tt.build(t, cfg)
			e, jobs, sink := testEngineWithTracking(t, cfg, dumper, backer)

			var heldLock *lock.Lock
			if tt.preAcquireLock {
				l, err := lock.Acquire(cfg.Backup.LockFile)
				if err != nil {
					t.Fatalf("setup: acquire lock: %v", err)
				}
				heldLock = l
				defer heldLock.Release()
			}

			result, err := e.Run(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
			if result.JobID == "" {
				t.Fatal("Result.JobID is empty -- job tracking should always create a record when a job store is configured")
			}

			recorded, getErr := jobs.Get(context.Background(), result.JobID)
			if getErr != nil {
				t.Fatalf("jobs.Get(%s): %v", result.JobID, getErr)
			}
			if recorded.Type != job.TypeBackup {
				t.Errorf("job type = %s, want %s", recorded.Type, job.TypeBackup)
			}
			if recorded.State != tt.wantJobState {
				t.Errorf("job state = %s, want %s", recorded.State, tt.wantJobState)
			}
			if tt.wantErrorCategory != "" && recorded.ErrorCategory != tt.wantErrorCategory {
				t.Errorf("job error category = %s, want %s", recorded.ErrorCategory, tt.wantErrorCategory)
			}
			if tt.wantJobState.Terminal() && recorded.ErrorSummary == "" && tt.wantErr {
				t.Error("a failed/cancelled job should carry a non-empty ErrorSummary")
			}

			got := sink.Events()
			for _, want := range tt.wantEvents {
				if !hasEventType(got, want) {
					t.Errorf("missing expected event %s; got %v", want, eventTypesOf(got))
				}
			}
			for _, absent := range tt.wantAbsentEvents {
				if hasEventType(got, absent) {
					t.Errorf("unexpected event %s present; got %v", absent, eventTypesOf(got))
				}
			}
			for _, e := range got {
				if e.JobID != result.JobID {
					t.Errorf("event %s has JobID %q, want %q", e.Type, e.JobID, result.JobID)
				}
			}
		})
	}
}

// TestEngine_Run_WithoutJobStore_DegradesGracefully proves the "missing
// configuration degrades safely" half of the package doc comment's
// policy: an Engine with no job store configured runs exactly as it did
// before this milestone -- no panic, no error, empty Result.JobID.
func TestEngine_Run_WithoutJobStore_DegradesGracefully(t *testing.T) {
	cfg := testConfig(t)
	dumpPath := filepath.Join(t.TempDir(), "app.dump.zst")
	if err := os.WriteFile(dumpPath, []byte("dump"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	dumper := &fakeDumper{dumpMeta: postgres.Metadata{Path: dumpPath, Bytes: 4}}
	backer := &fakeBacker{summary: restic.Summary{SnapshotID: "abc123"}}
	e := testEngine(t, cfg, dumper, backer) // jobs/events left as nil, exactly as before this milestone

	result, err := e.Run(context.Background())
	if err != nil {
		t.Fatalf("Run(): unexpected error: %v", err)
	}
	if result.JobID != "" {
		t.Errorf("Result.JobID = %q, want empty when no job store is configured", result.JobID)
	}
}

// TestEngine_Run_JobCreateFailure_StillRunsBackup proves the "a
// configured store failing at runtime degrades safely" half of the
// policy: a job store that fails on Create must not prevent the backup
// itself from succeeding.
func TestEngine_Run_JobCreateFailure_StillRunsBackup(t *testing.T) {
	cfg := testConfig(t)
	dumpPath := filepath.Join(t.TempDir(), "app.dump.zst")
	if err := os.WriteFile(dumpPath, []byte("dump"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	jobs := newTestJobStore(t)
	// Close the store immediately so every subsequent Create/Advance call
	// fails, simulating a broken job store without needing to fabricate
	// a specific SQLite error.
	jobs.Close()

	dumper := &fakeDumper{dumpMeta: postgres.Metadata{Path: dumpPath, Bytes: 4}}
	backer := &fakeBacker{summary: restic.Summary{SnapshotID: "abc123"}}
	e := &Engine{
		cfg:      cfg,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		postgres: dumper,
		restic:   backer,
		jobs:     jobs,
		events:   &event.InMemorySink{},
	}

	result, err := e.Run(context.Background())
	if err != nil {
		t.Fatalf("Run(): unexpected error despite a broken job store: %v", err)
	}
	if result.SnapshotID != "abc123" {
		t.Errorf("SnapshotID = %q, want %q -- the backup itself must succeed even when job tracking is broken", result.SnapshotID, "abc123")
	}
}
