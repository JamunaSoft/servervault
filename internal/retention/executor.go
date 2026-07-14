package retention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/event"
	"github.com/JamunaSoft/servervault/internal/job"
	"github.com/JamunaSoft/servervault/internal/lock"
	"github.com/JamunaSoft/servervault/internal/restic"
)

const errorSummaryMaxLen = 500

// Executor executes a Plan built by Planner: acquires a dedicated
// retention lock, refuses to run alongside a backup or a restore,
// revalidates the plan's removal set immediately before writing
// anything, performs the prune, and records a terminal job state on
// every exit path -- success, failure, or cancellation.
type Executor struct {
	restic ResticClient
	cfg    *config.Config
	jobs   *job.Store
	events event.Sink
	logger *slog.Logger
}

// NewExecutor builds an Executor. jobs must be non-nil: every prune
// appears in job history unconditionally -- the same "job/event
// tracking is required, not optional" choice internal/restore makes,
// for the same reason: this operation is destructive, and an
// unconditionally-recorded audit trail matters more here than it does
// for internal/backup's core safety properties. events defaults to
// event.NoopSink{} and logger to slog.Default() when nil.
func NewExecutor(resticClient ResticClient, cfg *config.Config, jobs *job.Store, events event.Sink, logger *slog.Logger) (*Executor, error) {
	if resticClient == nil {
		return nil, fmt.Errorf("retention: executor: restic client must not be nil")
	}
	if cfg == nil {
		return nil, fmt.Errorf("retention: executor: config must not be nil")
	}
	if jobs == nil {
		return nil, fmt.Errorf("retention: executor: job store must not be nil")
	}
	if events == nil {
		events = event.NoopSink{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Executor{restic: resticClient, cfg: cfg, jobs: jobs, events: events, logger: logger}, nil
}

// Result summarizes a completed prune.
type Result struct {
	JobID              string
	RemovedSnapshotIDs []string
	RemovedCount       int
	Duration           time.Duration
}

// Execute runs plan to completion. Every exit path -- success, any
// failure, or ctx cancellation -- releases the retention lock and
// records a terminal job state.
//
// Before performing the one destructive call it ever makes (a real
// `restic forget --prune`), Execute recomputes the plan from scratch
// (the same Snapshots -> Check -> dry-run Forget -> safety-limit
// pipeline Plan itself runs) and compares the fresh removal set against
// plan's. Any difference -- a backup added a new snapshot, another
// prune already ran, the repository's health changed -- fails the run
// with *ErrPlanStale rather than proceeding on a decision that may no
// longer be accurate. This mirrors internal/restore's Executor
// revalidating its own critical assumptions immediately before writing.
func (x *Executor) Execute(ctx context.Context, plan Plan) (Result, error) {
	start := time.Now()

	l, ok, err := lock.TryAcquire(x.cfg.Retention.LockFile)
	if err != nil {
		return Result{}, fmt.Errorf("retention: acquire lock: %w", err)
	}
	if !ok {
		return Result{}, lock.ErrLocked
	}
	defer l.Release()

	if held, _, err := lock.Status(x.cfg.Backup.LockFile); err != nil {
		return Result{}, fmt.Errorf("retention: check backup lock status: %w", err)
	} else if held {
		return Result{}, ErrBackupInProgress
	}
	if held, _, err := lock.Status(x.cfg.Restore.LockFile); err != nil {
		return Result{}, fmt.Errorf("retention: check restore lock status: %w", err)
	} else if held {
		return Result{}, ErrRestoreInProgress
	}

	j, err := x.jobs.Create(ctx, job.Job{Type: job.TypePrune, Metadata: job.Metadata{HostTag: x.cfg.HostTag}})
	if err != nil {
		return Result{}, fmt.Errorf("retention: create job record: %w", err)
	}

	x.emit(ctx, event.TypeJobCreated, j.ID, event.SeverityInfo, event.Metadata{})
	x.emit(ctx, event.TypeRetentionPlanned, j.ID, event.SeverityInfo, event.Metadata{SnapshotsRemoved: plan.RemoveCount})

	// fail records a terminal job state and event, using a
	// cancellation-independent context so cleanup and bookkeeping still
	// happen even when the original ctx is what caused the failure --
	// see cleanupContext's doc comment.
	fail := func(cat job.ErrorCategory, cause error) (Result, error) {
		cctx := cleanupContext(ctx)
		state := terminalStateFor(ctx, cause)
		summary := boundedSummary(cause)
		if _, advErr := x.jobs.Advance(cctx, j.ID, state, job.AdvanceOptions{ErrorCategory: cat, ErrorSummary: summary}); advErr != nil {
			x.logger.Warn("retention: failed to record terminal job state", "job_id", j.ID, "error", advErr)
		}
		evType := event.TypeJobFailed
		if state == job.StateCancelled {
			evType = event.TypeJobCancelled
		}
		x.emit(cctx, evType, j.ID, event.SeverityError, event.Metadata{ErrorCategory: string(cat), ErrorSummary: summary})
		return Result{JobID: j.ID}, cause
	}

	if _, err := x.jobs.Advance(ctx, j.ID, job.StatePreparing, job.AdvanceOptions{}); err != nil {
		return fail(job.ErrorCategoryExecution, fmt.Errorf("retention: advance job to preparing: %w", err))
	}
	x.emit(ctx, event.TypeRetentionStarted, j.ID, event.SeverityInfo, event.Metadata{})

	if _, err := x.jobs.Advance(ctx, j.ID, job.StateVerifying, job.AdvanceOptions{}); err != nil {
		return fail(job.ErrorCategoryExecution, fmt.Errorf("retention: advance job to verifying: %w", err))
	}

	planner, err := NewPlanner(x.restic, x.cfg)
	if err != nil {
		return fail(job.ErrorCategoryExecution, fmt.Errorf("retention: revalidate: %w", err))
	}
	fresh, err := planner.Plan(ctx)
	if err != nil {
		return fail(categorize(err), fmt.Errorf("retention: revalidate: %w", err))
	}
	if fresh.RemoveCount != plan.RemoveCount || !equalStrings(fresh.RemoveSnapshotIDs, plan.RemoveSnapshotIDs) {
		return fail(job.ErrorCategoryValidation, &ErrPlanStale{
			Reason: fmt.Sprintf("planned removal set changed since planning (was %d snapshot(s), now %d)", plan.RemoveCount, fresh.RemoveCount),
		})
	}

	x.logger.Info("retention started", "operation", "retention", "host_tag", x.cfg.HostTag, "planned_removals", plan.RemoveCount)

	if _, err := x.jobs.Advance(ctx, j.ID, job.StateBackingUp, job.AdvanceOptions{}); err != nil {
		return fail(job.ErrorCategoryExecution, fmt.Errorf("retention: advance job to backing_up: %w", err))
	}

	tags := planner.tags()
	summary, err := x.restic.Forget(ctx, restic.ForgetOptions{
		Host:        x.cfg.HostTag,
		Tags:        tags,
		KeepDaily:   x.cfg.Retention.KeepDaily,
		KeepWeekly:  x.cfg.Retention.KeepWeekly,
		KeepMonthly: x.cfg.Retention.KeepMonthly,
		Prune:       true,
	})
	if err != nil {
		return fail(categorize(err), fmt.Errorf("retention: forget: %w", err))
	}

	removedCount := len(summary.RemovedSnapshotIDs)
	meta := job.Metadata{HostTag: x.cfg.HostTag, SnapshotsRemoved: removedCount}
	if _, err := x.jobs.Advance(ctx, j.ID, job.StateCompleted, job.AdvanceOptions{Metadata: &meta}); err != nil {
		x.logger.Warn("retention: failed to record completed job state", "job_id", j.ID, "error", err)
	}
	x.emit(ctx, event.TypeRetentionCompleted, j.ID, event.SeverityInfo, event.Metadata{SnapshotsRemoved: removedCount})

	result := Result{
		JobID:              j.ID,
		RemovedSnapshotIDs: summary.RemovedSnapshotIDs,
		RemovedCount:       removedCount,
		Duration:           time.Since(start),
	}

	x.logger.Info("retention completed", "operation", "retention", "snapshots_removed", removedCount, "duration", result.Duration)
	return result, nil
}

// categorize maps an execution error to a job.ErrorCategory for job
// history and events.
func categorize(err error) job.ErrorCategory {
	var stale *ErrPlanStale
	if errors.As(err, &stale) {
		return job.ErrorCategoryValidation
	}
	var unhealthy *ErrRepositoryUnhealthy
	if errors.As(err, &unhealthy) {
		return job.ErrorCategoryValidation
	}
	var maxExceeded *ErrMaxDeleteExceeded
	if errors.As(err, &maxExceeded) {
		return job.ErrorCategoryValidation
	}
	if errors.Is(err, ErrBelowMinimumSnapshots) {
		return job.ErrorCategoryValidation
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return job.ErrorCategoryCancelled
	}
	return job.ErrorCategoryExecution
}

// terminalStateFor decides whether a failure should be recorded as
// StateCancelled or StateFailed -- mirrors internal/restore's
// identically-reasoned helper.
func terminalStateFor(ctx context.Context, err error) job.State {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return job.StateCancelled
	}
	return job.StateFailed
}

// cleanupContext returns a context that carries ctx's values but is
// never itself cancelled, for cleanup operations that must still run
// even when ctx is the reason execution stopped. Mirrors
// internal/backup and internal/restore's identically-reasoned helper.
func cleanupContext(ctx context.Context) context.Context {
	return context.WithoutCancel(ctx)
}

// boundedSummary renders err as operator-facing text for ErrorSummary
// and event metadata, truncated to a bounded length. Safe with respect
// to secrets by construction of the packages err can originate from --
// internal/restic never passes the repository password as a CLI
// argument or embeds it in error text (RESTIC_PASSWORD_FILE carries a
// filesystem path, never file contents) -- mirrors internal/backup and
// internal/restore's identically-reasoned helper.
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
		x.logger.Warn("retention: failed to construct event", "error", err)
		return
	}
	if err := x.events.Emit(ctx, e); err != nil {
		x.logger.Warn("retention: failed to emit event", "error", err, "event_type", string(typ))
	}
}

// equalStrings reports whether a and b contain the same elements in the
// same order -- used to compare a revalidated removal set against the
// originally planned one. Order is significant here on purpose: restic
// reports keep/remove lists in a stable order for an unchanged
// repository, so an order difference is itself a meaningful signal that
// something changed, not just noise to normalize away.
func equalStrings(a, b []string) bool {
	return reflect.DeepEqual(a, b)
}
