// Package backup orchestrates one backup run: acquire a lock, dump and
// verify PostgreSQL (if enabled), back up with Restic, clean up. It knows
// the state machine; it does not know how to talk to PostgreSQL or Restic
// itself -- that's internal/postgres and internal/restic, injected as
// small interfaces (Dumper, Backer) so orchestration logic is testable
// independent of subprocess argv construction.
//
// Engine.Run has no knowledge of Cobra or any other CLI concern (see
// CLAUDE.md: "business logic must not depend on Cobra") -- it is a plain
// function callable from a CLI command today and, unmodified, from a job
// queue or an agent later.
//
// Job/event tracking (internal/job, internal/event) is optional,
// configured via WithJobStore/WithEventSink. This is a deliberate
// asymmetry with internal/restore, which requires a job store
// unconditionally: restore's cleanup-ownership tracking (has this
// specific run created a temporary database it must drop on failure?)
// depends on job/event infrastructure more directly, while backup's
// core safety properties (lock, verify-before-restic, cleanup on every
// exit path) do not. Concretely: missing configuration (WithJobStore
// never called) and a configured store failing at runtime are both
// handled the same way -- logged as a warning, never as a reason to
// fail the backup itself. A backup tool that stopped backing things up
// because a bookkeeping database had a problem would be a worse outcome
// than a backup that succeeds without a job record.
package backup

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
	"github.com/JamunaSoft/servervault/internal/execx"
	"github.com/JamunaSoft/servervault/internal/job"
	"github.com/JamunaSoft/servervault/internal/lock"
	"github.com/JamunaSoft/servervault/internal/postgres"
	"github.com/JamunaSoft/servervault/internal/restic"
)

// Dumper is the subset of *postgres.Client the orchestrator needs.
type Dumper interface {
	Ping(ctx context.Context) error
	Dump(ctx context.Context, dir string) (postgres.Metadata, error)
	VerifyDump(ctx context.Context, dumpPath string) error
}

// Backer is the subset of *restic.Repository the orchestrator needs.
type Backer interface {
	Backup(ctx context.Context, opts restic.BackupOptions) (restic.Summary, error)
}

// Result summarizes a completed backup.
type Result struct {
	SnapshotID   string
	StartedAt    time.Time
	FinishedAt   time.Time
	Duration     time.Duration
	DumpBytes    int64
	FilesNew     int
	FilesChanged int
	// Warnings carries non-fatal issues from the run -- currently just
	// Restic's per-file read errors on an otherwise-successful backup
	// (see restic.Summary.Warnings).
	Warnings []string
	// JobID is the internal/job record created for this run, empty if
	// no job store was configured (see WithJobStore) or if creating the
	// record itself failed.
	JobID string
}

// Engine runs backups for one configured server.
type Engine struct {
	cfg      *config.Config
	logger   *slog.Logger
	postgres Dumper // nil when Postgres is disabled
	restic   Backer
	jobs     *job.Store // nil disables job-history tracking entirely
	events   event.Sink // never nil once New has run; defaults to event.NoopSink{}
}

// Option configures optional Engine behavior not covered by New's
// required parameters.
type Option func(*Engine)

// WithJobStore enables job-history tracking for every Run call: each
// run creates a job record and advances it through the typed lifecycle
// (see internal/job). Without this option, Run still executes exactly
// as before -- it simply does not create or update any job record. See
// the package doc comment for why this is optional rather than
// required, unlike internal/restore.
func WithJobStore(s *job.Store) Option {
	return func(e *Engine) { e.jobs = s }
}

// WithEventSink enables structured event emission alongside job
// tracking (see internal/event). Has no effect unless WithJobStore is
// also set -- events are emitted per job phase, so there is nothing to
// attach an event to without a job. Defaults to event.NoopSink{} if
// never called.
func WithEventSink(s event.Sink) Option {
	return func(e *Engine) { e.events = s }
}

// New builds an Engine. runner is the execx.Runner used to construct the
// underlying Restic/PostgreSQL clients; logger defaults to slog.Default()
// if nil. opts is empty for every caller that existed before v0.3.5's
// job/event integration -- WithJobStore/WithEventSink are strictly
// additive.
func New(cfg *config.Config, logger *slog.Logger, runner execx.Runner, opts ...Option) (*Engine, error) {
	if cfg == nil {
		return nil, fmt.Errorf("backup: config must not be nil")
	}
	if cfg.Backup.LockFile == "" {
		return nil, fmt.Errorf("backup: backup.lock_file must be configured")
	}
	if logger == nil {
		logger = slog.Default()
	}

	e := &Engine{
		cfg:    cfg,
		logger: logger,
		restic: restic.New(runner, cfg.Restic),
		events: event.NoopSink{},
	}
	if cfg.Postgres.Enabled {
		e.postgres = postgres.New(runner, cfg.Postgres)
	}
	for _, opt := range opts {
		opt(e)
	}
	if e.events == nil {
		e.events = event.NoopSink{}
	}
	return e, nil
}

// Run executes one backup end to end: acquire lock, (ping, dump, verify
// PostgreSQL if enabled), Restic backup, release lock. Every exit path --
// success, any failure, or ctx cancellation -- releases the lock and
// removes the local dump file via defer; see the failure/cleanup matrix in
// the design notes (docs/backup-flow.md).
//
// When job/event tracking is configured (see WithJobStore/WithEventSink),
// every call creates a job record and advances it through
// pending -> preparing -> [dumping -> verifying] -> backing_up ->
// completed, or to failed/cancelled on any exit path that isn't a clean
// success -- see docs/job-lifecycle.md. This never changes Run's own
// observable behavior or return values: job/event bookkeeping is a side
// effect layered on top of exactly the same control flow, not a
// replacement for it.
//
// Run is safe to retry after any failure: a failed run leaves no partial
// state visible to Restic (the dump never reaches the repository unless
// verification passed), and re-running acquires a fresh lock. It is not
// idempotent in the sense of producing the same snapshot ID twice --
// Restic's own deduplication makes a content-identical rerun cheap and
// safe, not an error, but it is still a new snapshot.
func (e *Engine) Run(ctx context.Context) (Result, error) {
	start := time.Now()

	jobID, track := e.createJob(ctx)

	// "preparing" covers everything from here through a successful lock
	// acquisition -- getting ready to run, including the lock attempt
	// itself. StatePending has no direct edge to StateFailed (a job
	// that never left pending was never far enough along to have
	// "failed" at anything); advancing to preparing first, before the
	// lock attempt, is what lets a lock-busy run still be recorded as a
	// legally-reached failed job rather than being stuck showing
	// pending forever.
	e.advanceJob(ctx, track, jobID, job.StatePreparing)
	e.emit(ctx, track, event.TypeJobStarted, jobID, event.SeverityInfo, event.Metadata{})

	l, ok, err := lock.TryAcquire(e.cfg.Backup.LockFile)
	if err != nil {
		e.failJob(ctx, track, jobID, job.ErrorCategoryLock, err)
		return Result{JobID: jobID}, fmt.Errorf("backup: acquire lock: %w", err)
	}
	if !ok {
		e.failJob(ctx, track, jobID, job.ErrorCategoryLock, lock.ErrLocked)
		return Result{JobID: jobID}, lock.ErrLocked
	}
	defer l.Release()

	e.logger.Info("backup started", "operation", "backup", "host_tag", e.cfg.HostTag)

	var dumpPath string
	var dumpBytes int64

	if e.postgres != nil {
		if err := e.postgres.Ping(ctx); err != nil {
			e.failJob(ctx, track, jobID, job.ErrorCategoryConnectivity, err)
			return Result{JobID: jobID}, fmt.Errorf("backup: postgres ping: %w", err)
		}

		dumpDir := filepath.Join(e.cfg.Backup.Root, "postgresql")
		if err := os.MkdirAll(dumpDir, 0o700); err != nil {
			e.failJob(ctx, track, jobID, job.ErrorCategoryExecution, err)
			return Result{JobID: jobID}, fmt.Errorf("backup: create dump directory: %w", err)
		}

		e.advanceJob(ctx, track, jobID, job.StateDumping)
		e.emit(ctx, track, event.TypeDatabaseDumpStarted, jobID, event.SeverityInfo, event.Metadata{})

		meta, err := e.postgres.Dump(ctx, dumpDir)
		if meta.Path != "" {
			// Unconditional cleanup, registered as soon as a path
			// exists -- fires on success, any later failure, and
			// cancellation alike. The dump is never needed locally
			// once Restic has (or has not) backed it up.
			defer os.Remove(meta.Path)
		}
		if err != nil {
			e.failJob(ctx, track, jobID, job.ErrorCategoryExecution, err)
			return Result{JobID: jobID}, fmt.Errorf("backup: dump: %w", err)
		}
		dumpPath = meta.Path
		dumpBytes = meta.Bytes
		e.emit(ctx, track, event.TypeDatabaseDumpCompleted, jobID, event.SeverityInfo, event.Metadata{BytesTotal: dumpBytes})

		e.advanceJob(ctx, track, jobID, job.StateVerifying)
		e.emit(ctx, track, event.TypeVerificationStarted, jobID, event.SeverityInfo, event.Metadata{})

		if err := e.postgres.VerifyDump(ctx, dumpPath); err != nil {
			// Verification failed: Restic is never called. The dump
			// file is still removed by the defer above.
			e.failJob(ctx, track, jobID, job.ErrorCategoryVerification, err)
			return Result{JobID: jobID}, fmt.Errorf("backup: verify dump: %w", err)
		}
		e.emit(ctx, track, event.TypeVerificationCompleted, jobID, event.SeverityInfo, event.Metadata{})
		e.logger.Info("dump verified", "operation", "backup", "bytes", dumpBytes)
	}

	paths := make([]string, 0, len(e.cfg.Backup.Paths)+1)
	paths = append(paths, e.cfg.Backup.Paths...)
	if dumpPath != "" {
		paths = append(paths, dumpPath)
	}

	tags := make([]string, 0, len(e.cfg.Restic.Tags)+1)
	tags = append(tags, "servervault")
	tags = append(tags, e.cfg.Restic.Tags...)

	// preparing -> backing_up directly (Postgres disabled) or
	// verifying -> backing_up (Postgres enabled) are both legal --
	// see internal/job's transition graph.
	e.advanceJob(ctx, track, jobID, job.StateBackingUp)
	e.emit(ctx, track, event.TypeBackupStarted, jobID, event.SeverityInfo, event.Metadata{})

	summary, err := e.restic.Backup(ctx, restic.BackupOptions{
		Paths:       paths,
		ExcludeFile: e.cfg.Backup.ExcludeFile,
		Tags:        tags,
		HostTag:     e.cfg.HostTag,
	})
	if err != nil {
		e.failJob(ctx, track, jobID, job.ErrorCategoryExecution, err)
		return Result{JobID: jobID}, fmt.Errorf("backup: restic backup: %w", err)
	}
	e.emit(ctx, track, event.TypeBackupCompleted, jobID, event.SeverityInfo, event.Metadata{
		SnapshotID: summary.SnapshotID, BytesTotal: summary.BytesAdded,
		FilesNew: summary.FilesNew, FilesChanged: summary.FilesChanged,
	})

	result := Result{
		SnapshotID:   summary.SnapshotID,
		StartedAt:    start,
		FinishedAt:   time.Now(),
		Duration:     time.Since(start),
		DumpBytes:    dumpBytes,
		FilesNew:     summary.FilesNew,
		FilesChanged: summary.FilesChanged,
		Warnings:     summary.Warnings,
		JobID:        jobID,
	}

	e.completeJob(ctx, track, jobID, job.Metadata{
		HostTag: e.cfg.HostTag, SnapshotID: summary.SnapshotID,
		BytesTotal: summary.BytesAdded, FilesNew: summary.FilesNew, FilesChanged: summary.FilesChanged,
	})

	e.logger.Info("backup completed", "operation", "backup",
		"snapshot_id", result.SnapshotID, "duration", result.Duration, "warnings", len(result.Warnings))
	return result, nil
}

// createJob creates a job record for this run, if a job store is
// configured. track reports whether job/event tracking should happen
// for the rest of this run -- false whenever no store is configured, or
// whenever creating the record itself failed (logged, not fatal: a
// bookkeeping failure never blocks a backup -- see the package doc
// comment).
func (e *Engine) createJob(ctx context.Context) (jobID string, track bool) {
	if e.jobs == nil {
		return "", false
	}
	j, err := e.jobs.Create(ctx, job.Job{Type: job.TypeBackup, Metadata: job.Metadata{HostTag: e.cfg.HostTag}})
	if err != nil {
		e.logger.Warn("backup: failed to create job record; continuing without job tracking", "error", err)
		return "", false
	}
	e.emit(ctx, true, event.TypeJobCreated, j.ID, event.SeverityInfo, event.Metadata{})
	return j.ID, true
}

// advanceJob is a no-op when track is false. A failure to advance is
// logged, never returned -- see the package doc comment on why job
// bookkeeping degrades safely instead of failing the backup.
func (e *Engine) advanceJob(ctx context.Context, track bool, jobID string, to job.State) {
	if !track {
		return
	}
	if _, err := e.jobs.Advance(ctx, jobID, to, job.AdvanceOptions{}); err != nil {
		e.logger.Warn("backup: failed to advance job state", "job_id", jobID, "target_state", string(to), "error", err)
	}
}

// completeJob is a no-op when track is false.
func (e *Engine) completeJob(ctx context.Context, track bool, jobID string, meta job.Metadata) {
	if !track {
		return
	}
	if _, err := e.jobs.Advance(ctx, jobID, job.StateCompleted, job.AdvanceOptions{Metadata: &meta}); err != nil {
		e.logger.Warn("backup: failed to record completed job state", "job_id", jobID, "error", err)
	}
}

// failJob records a terminal job state (cancelled, distinct from
// failed, if ctx or cause indicates cancellation) and emits the
// matching event. It is a no-op when track is false. Cleanup
// bookkeeping runs under a context that carries the original's values
// but is never itself cancelled (context.WithoutCancel), the same
// reasoning internal/restore's executor uses: without it, a cancelled
// ctx would make this bookkeeping call fail immediately, silently
// skipping the record-keeping that cancellation made necessary.
func (e *Engine) failJob(ctx context.Context, track bool, jobID string, cat job.ErrorCategory, cause error) {
	if !track {
		return
	}
	cctx := context.WithoutCancel(ctx)
	state := job.StateFailed
	if ctx.Err() != nil || errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		state = job.StateCancelled
		cat = job.ErrorCategoryCancelled
	}
	summary := boundedSummary(cause)

	if _, err := e.jobs.Advance(cctx, jobID, state, job.AdvanceOptions{ErrorCategory: cat, ErrorSummary: summary}); err != nil {
		e.logger.Warn("backup: failed to record terminal job state", "job_id", jobID, "error", err)
	}

	evType := event.TypeJobFailed
	if state == job.StateCancelled {
		evType = event.TypeJobCancelled
	}
	e.emit(cctx, true, evType, jobID, event.SeverityError, event.Metadata{ErrorCategory: string(cat), ErrorSummary: summary})
}

// emit is a no-op when track is false. A failure to emit is logged,
// never returned.
func (e *Engine) emit(ctx context.Context, track bool, typ event.Type, jobID string, sev event.Severity, meta event.Metadata) {
	if !track || e.events == nil {
		return
	}
	ev, err := event.New(typ, jobID, sev, meta)
	if err != nil {
		e.logger.Warn("backup: failed to construct event", "error", err)
		return
	}
	if err := e.events.Emit(ctx, ev); err != nil {
		e.logger.Warn("backup: failed to emit event", "error", err, "event_type", string(typ))
	}
}

const errorSummaryMaxLen = 500

// boundedSummary renders err as operator-facing text for ErrorSummary
// and event metadata, truncated to a bounded length. Safe with respect
// to secrets by construction of the packages err can originate from,
// not by redaction: internal/restic and internal/postgres never pass
// password/credential material as a CLI argument or embed it in a
// configured repository URL -- RESTIC_PASSWORD_FILE carries a
// filesystem path, never file contents, and PostgreSQL authentication
// is passwordless peer auth (see both packages' doc comments) -- so
// subprocess stderr text wrapped into an error here cannot contain the
// secret itself. Truncation exists to keep history readable, not to
// redact. Mirrors internal/restore's identically-reasoned helper.
func boundedSummary(err error) string {
	s := err.Error()
	if len(s) > errorSummaryMaxLen {
		return s[:errorSummaryMaxLen] + "... (truncated)"
	}
	return s
}
