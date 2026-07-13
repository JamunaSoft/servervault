package restore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/event"
	"github.com/JamunaSoft/servervault/internal/job"
	"github.com/JamunaSoft/servervault/internal/lock"
	"github.com/JamunaSoft/servervault/internal/restic"
)

// ErrBackupInProgress is returned by Execute when a backup currently
// holds its lock. Restore does not queue or wait -- it fails fast, the
// same "fail fast rather than serialize" choice internal/lock already
// makes for concurrent backups. See docs/restore-flow.md for why restore
// checks the backup lock's status rather than sharing a lock file with
// it.
var ErrBackupInProgress = errors.New("restore: a backup is currently running; wait for it to complete before restoring")

const errorSummaryMaxLen = 500

// Executor executes a Plan built by Planner: acquires a dedicated
// restore lock, revalidates the plan's critical assumptions immediately
// before writing anything, performs the restore, verifies the result,
// and cleans up on every exit path -- success, failure, or
// cancellation.
type Executor struct {
	restic   ResticClient
	postgres PostgresClient // nil when Postgres is disabled; only touched for TargetTempDB
	cfg      *config.Config
	jobs     *job.Store
	events   event.Sink
	logger   *slog.Logger
}

// NewExecutor builds an Executor. jobs must be non-nil: every restore
// appears in job history unconditionally, it is not an optional feature
// -- see docs/restore-flow.md. events defaults to event.NoopSink{} and
// logger to slog.Default() when nil.
func NewExecutor(resticClient ResticClient, postgresClient PostgresClient, cfg *config.Config, jobs *job.Store, events event.Sink, logger *slog.Logger) (*Executor, error) {
	if resticClient == nil {
		return nil, fmt.Errorf("restore: executor: restic client must not be nil")
	}
	if cfg == nil {
		return nil, fmt.Errorf("restore: executor: config must not be nil")
	}
	if jobs == nil {
		return nil, fmt.Errorf("restore: executor: job store must not be nil")
	}
	if events == nil {
		events = event.NoopSink{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Executor{
		restic:   resticClient,
		postgres: postgresClient,
		cfg:      cfg,
		jobs:     jobs,
		events:   events,
		logger:   logger,
	}, nil
}

// Result summarizes a completed restore.
type Result struct {
	JobID string
	// Destination is the staging directory (TargetFiles) or the
	// temporary database name (TargetTempDB) the restore landed in.
	Destination   string
	FilesRestored int64
	BytesRestored int64
	Duration      time.Duration
}

// Execute runs plan to completion. Every exit path -- success, any
// failure, or ctx cancellation -- releases the restore lock, records a
// terminal job state, and cleans up any partial local state this call
// itself created (a partially extracted staging directory is marked
// incomplete rather than deleted; a temporary database this call created
// is dropped, but only if this call created it -- see executeTempDB).
func (x *Executor) Execute(ctx context.Context, plan Plan) (Result, error) {
	start := time.Now()

	l, ok, err := lock.TryAcquire(x.cfg.Restore.LockFile)
	if err != nil {
		return Result{}, fmt.Errorf("restore: acquire lock: %w", err)
	}
	if !ok {
		return Result{}, lock.ErrLocked
	}
	defer l.Release()

	if held, _, err := lock.Status(x.cfg.Backup.LockFile); err != nil {
		return Result{}, fmt.Errorf("restore: check backup lock status: %w", err)
	} else if held {
		return Result{}, ErrBackupInProgress
	}

	j, err := x.jobs.Create(ctx, job.Job{
		Type: job.TypeRestore,
		Metadata: job.Metadata{
			SnapshotID:   plan.SnapshotID,
			TargetPath:   plan.Destination,
			DatabaseName: plan.TempDatabaseName,
		},
	})
	if err != nil {
		return Result{}, fmt.Errorf("restore: create job record: %w", err)
	}

	x.emit(ctx, event.TypeJobCreated, j.ID, event.SeverityInfo, event.Metadata{SnapshotID: plan.SnapshotID})
	x.emit(ctx, event.TypeRestorePlanned, j.ID, event.SeverityInfo, event.Metadata{
		SnapshotID: plan.SnapshotID, TargetPath: plan.Destination, DatabaseName: plan.TempDatabaseName,
	})

	result := Result{JobID: j.ID, Destination: plan.Destination}
	if plan.Target == TargetTempDB {
		result.Destination = plan.TempDatabaseName
	}

	// fail records a terminal job state and event, using a
	// cancellation-independent context so cleanup and bookkeeping still
	// happen even when the original ctx is what caused the failure --
	// see the doc comment on cleanupContext.
	fail := func(cat job.ErrorCategory, cause error) (Result, error) {
		cctx := cleanupContext(ctx)
		state := terminalStateFor(ctx, cause)
		summary := boundedSummary(cause)
		if _, advErr := x.jobs.Advance(cctx, j.ID, state, job.AdvanceOptions{ErrorCategory: cat, ErrorSummary: summary}); advErr != nil {
			x.logger.Warn("restore: failed to record terminal job state", "job_id", j.ID, "error", advErr)
		}
		evType := event.TypeJobFailed
		if state == job.StateCancelled {
			evType = event.TypeJobCancelled
		}
		x.emit(cctx, evType, j.ID, event.SeverityError, event.Metadata{ErrorCategory: string(cat), ErrorSummary: summary})
		return Result{JobID: j.ID}, cause
	}

	if _, err := x.jobs.Advance(ctx, j.ID, job.StatePreparing, job.AdvanceOptions{}); err != nil {
		return fail(job.ErrorCategoryExecution, fmt.Errorf("restore: advance job to preparing: %w", err))
	}
	x.emit(ctx, event.TypeRestoreStarted, j.ID, event.SeverityInfo, event.Metadata{SnapshotID: plan.SnapshotID})

	if err := x.revalidate(ctx, plan); err != nil {
		return fail(job.ErrorCategoryValidation, err)
	}

	if _, err := x.jobs.Advance(ctx, j.ID, job.StateBackingUp, job.AdvanceOptions{}); err != nil {
		return fail(job.ErrorCategoryExecution, fmt.Errorf("restore: advance job to backing_up: %w", err))
	}

	var filesRestored, bytesRestored int64
	var dbOwned bool

	switch plan.Target {
	case TargetFiles:
		filesRestored, bytesRestored, err = x.executeFiles(ctx, plan)
		if err != nil {
			markIncomplete(plan.Destination)
			return fail(categorize(err), err)
		}
	case TargetTempDB:
		filesRestored, bytesRestored, dbOwned, err = x.executeTempDB(ctx, plan)
		if err != nil {
			cctx := cleanupContext(ctx)
			if dbOwned {
				if dropErr := x.postgres.DropDatabase(cctx, plan.TempDatabaseName); dropErr != nil {
					x.logger.Warn("restore: failed to clean up temporary database after a failed restore", "database", plan.TempDatabaseName, "error", dropErr)
				}
			}
			_ = os.RemoveAll(plan.Destination)
			return fail(categorize(err), err)
		}
	}

	if _, err := x.jobs.Advance(ctx, j.ID, job.StateVerifying, job.AdvanceOptions{}); err != nil {
		return fail(job.ErrorCategoryExecution, fmt.Errorf("restore: advance job to verifying: %w", err))
	}

	if err := x.verify(ctx, plan); err != nil {
		cctx := cleanupContext(ctx)
		if plan.Target == TargetTempDB && dbOwned {
			if dropErr := x.postgres.DropDatabase(cctx, plan.TempDatabaseName); dropErr != nil {
				x.logger.Warn("restore: failed to clean up temporary database after a failed verification", "database", plan.TempDatabaseName, "error", dropErr)
			}
		}
		return fail(job.ErrorCategoryVerification, fmt.Errorf("restore: verify: %w", err))
	}

	if plan.Target == TargetTempDB {
		// The extracted dump file has already been loaded into the
		// database; the extraction directory served its purpose and
		// isn't the deliverable a user would inspect (unlike TargetFiles'
		// staging directory), so it's removed on success.
		_ = os.RemoveAll(plan.Destination)
	}

	meta := &job.Metadata{
		SnapshotID: plan.SnapshotID, TargetPath: plan.Destination, DatabaseName: plan.TempDatabaseName,
		BytesTotal: bytesRestored, FilesNew: int(filesRestored),
	}
	if _, err := x.jobs.Advance(ctx, j.ID, job.StateCompleted, job.AdvanceOptions{Metadata: meta}); err != nil {
		x.logger.Warn("restore: failed to record completed job state", "job_id", j.ID, "error", err)
	}
	x.emit(ctx, event.TypeRestoreCompleted, j.ID, event.SeverityInfo, event.Metadata{
		SnapshotID: plan.SnapshotID, BytesTotal: bytesRestored, FilesNew: int(filesRestored),
	})

	result.FilesRestored = filesRestored
	result.BytesRestored = bytesRestored
	result.Duration = time.Since(start)
	return result, nil
}

// revalidate re-checks the plan's critical assumptions immediately
// before Execute writes anything, catching the case where something
// changed in the time between Plan and Execute (a human or another
// process created the destination directory or database in the
// meantime).
func (x *Executor) revalidate(ctx context.Context, plan Plan) error {
	switch plan.Target {
	case TargetFiles:
		if _, err := os.Stat(plan.Destination); err == nil {
			return &ErrPlanStale{Reason: fmt.Sprintf("destination %q now exists", plan.Destination)}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("restore: check destination: %w", err)
		}
		for _, bp := range x.cfg.Backup.Paths {
			if config.PathsOverlap(plan.Destination, bp) {
				return &ErrPlanStale{Reason: "destination now overlaps a configured backup path"}
			}
		}
		return nil

	case TargetTempDB:
		if x.postgres == nil {
			return fmt.Errorf("restore: postgres client is not configured")
		}
		exists, err := x.postgres.DatabaseExists(ctx, plan.TempDatabaseName)
		if err != nil {
			return fmt.Errorf("restore: check temporary database: %w", err)
		}
		if exists {
			return &ErrPlanStale{Reason: fmt.Sprintf("database %q now exists", plan.TempDatabaseName)}
		}
		if plan.TempDatabaseName == x.cfg.Postgres.Database {
			return &ErrPlanStale{Reason: "temporary database name now equals the live database name"}
		}
		return nil

	default:
		return fmt.Errorf("restore: revalidate: unknown target %q", plan.Target)
	}
}

func (x *Executor) executeFiles(ctx context.Context, plan Plan) (files, bytes int64, err error) {
	if err := os.MkdirAll(plan.Destination, 0o700); err != nil {
		return 0, 0, fmt.Errorf("restore: create staging directory: %w", err)
	}
	summary, err := x.restic.Restore(ctx, restic.RestoreOptions{
		SnapshotID: plan.SnapshotID,
		Target:     plan.Destination,
		Include:    plan.RepositoryPath,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("restore: restic restore: %w", err)
	}
	return int64(summary.FilesRestored), summary.BytesRestored, nil
}

// executeTempDB extracts just the located dump file from the repository
// into an internal extraction directory, creates the temporary database,
// and restores into it. owned is true from the moment CreateDatabase
// succeeds -- from that point on, the caller must drop the database on
// any subsequent failure, since ServerVault (and only ServerVault, in
// this call) created it.
func (x *Executor) executeTempDB(ctx context.Context, plan Plan) (files, bytes int64, owned bool, err error) {
	if x.postgres == nil {
		return 0, 0, false, fmt.Errorf("restore: postgres client is not configured")
	}

	if err := os.MkdirAll(plan.Destination, 0o700); err != nil {
		return 0, 0, false, fmt.Errorf("restore: create extraction directory: %w", err)
	}
	if _, err := x.restic.Restore(ctx, restic.RestoreOptions{
		SnapshotID: plan.SnapshotID,
		Target:     plan.Destination,
		Include:    plan.RepositoryPath,
	}); err != nil {
		return 0, 0, false, fmt.Errorf("restore: extract dump from repository: %w", err)
	}

	extractedPath := filepath.Join(plan.Destination, plan.RepositoryPath)
	if _, statErr := os.Stat(extractedPath); statErr != nil {
		return 0, 0, false, fmt.Errorf("restore: extracted dump file not found at expected path %q: %w", extractedPath, statErr)
	}

	if err := x.postgres.CreateDatabase(ctx, plan.TempDatabaseName); err != nil {
		return 0, 0, false, fmt.Errorf("restore: create temporary database: %w", err)
	}
	owned = true

	if err := x.postgres.RestoreToTemp(ctx, extractedPath, plan.TempDatabaseName); err != nil {
		return 0, 0, owned, fmt.Errorf("restore: restore dump into temporary database: %w", err)
	}

	return 1, plan.ExpectedBytes, owned, nil
}

func (x *Executor) verify(ctx context.Context, plan Plan) error {
	switch plan.Target {
	case TargetFiles:
		entries, err := os.ReadDir(plan.Destination)
		if err != nil {
			return fmt.Errorf("read staging directory: %w", err)
		}
		if len(entries) == 0 && plan.ExpectedFiles > 0 {
			return fmt.Errorf("staging directory %q is empty but %d file(s) were expected", plan.Destination, plan.ExpectedFiles)
		}
		return nil
	case TargetTempDB:
		if x.postgres == nil {
			return fmt.Errorf("postgres client is not configured")
		}
		return x.postgres.PingDatabase(ctx, plan.TempDatabaseName)
	default:
		return fmt.Errorf("unknown target %q", plan.Target)
	}
}

// markIncomplete writes a small marker file into a partially-restored
// staging directory rather than deleting it -- see the package doc
// comment on requirement 18 (staging output must be clearly marked
// incomplete or removed safely). Marking is chosen over deletion: a
// staging directory is inert by construction (never a live path), so
// preserving a partial result for an operator to inspect is safer than
// silently discarding it. Best-effort: a failure to write the marker
// does not change the restore's own error.
func markIncomplete(destination string) {
	if destination == "" {
		return
	}
	if _, err := os.Stat(destination); err != nil {
		return // the directory itself was never created; nothing to mark
	}
	marker := filepath.Join(destination, ".incomplete")
	_ = os.WriteFile(marker, []byte("this restore did not complete successfully; contents may be partial or absent\n"), 0o600)
}

// categorize maps an execution error to a job.ErrorCategory for job
// history and events.
func categorize(err error) job.ErrorCategory {
	var stale *ErrPlanStale
	if errors.As(err, &stale) {
		return job.ErrorCategoryValidation
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return job.ErrorCategoryCancelled
	}
	return job.ErrorCategoryExecution
}

// terminalStateFor decides whether a failure should be recorded as
// StateCancelled (the operation was cancelled, via ctx or a wrapped
// context error) or StateFailed (everything else) -- see internal/job's
// "cancellation and interruption semantics must be explicit"
// requirement.
func terminalStateFor(ctx context.Context, err error) job.State {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return job.StateCancelled
	}
	return job.StateFailed
}

// cleanupContext returns a context that carries ctx's values but is
// never itself cancelled, for use by cleanup operations (dropping a
// database this call created, recording the job's terminal state) that
// must still run even when ctx is the reason execution stopped --
// without this, a cancelled ctx would cause cleanup's own database
// queries and subprocess calls to fail immediately (internal/execx and
// database/sql both check ctx.Err() before starting work), silently
// skipping the cleanup that cancellation itself made necessary.
func cleanupContext(ctx context.Context) context.Context {
	return context.WithoutCancel(ctx)
}

// boundedSummary renders err as operator-facing text for ErrorSummary
// and event metadata, truncated to a bounded length so persisted history
// stays readable. This is safe with respect to secrets by construction
// of the packages err can originate from, not by redaction: internal/
// restic and internal/postgres never pass password/credential material
// as a CLI argument or embed it in a configured repository URL --
// RESTIC_PASSWORD_FILE carries a filesystem path, never file contents,
// and PostgreSQL authentication is passwordless peer auth (see both
// packages' doc comments) -- so subprocess stderr text wrapped into an
// error here cannot contain the secret itself. Truncation exists to keep
// history readable, not to redact.
func boundedSummary(err error) string {
	s := err.Error()
	if len(s) > errorSummaryMaxLen {
		return s[:errorSummaryMaxLen] + "... (truncated)"
	}
	return s
}

func (x *Executor) emit(ctx context.Context, typ event.Type, jobID string, sev event.Severity, meta event.Metadata) {
	e, err := event.New(typ, jobID, sev, meta)
	if err != nil {
		x.logger.Warn("restore: failed to construct event", "error", err)
		return
	}
	if err := x.events.Emit(ctx, e); err != nil {
		x.logger.Warn("restore: failed to emit event", "error", err, "event_type", string(typ))
	}
}
