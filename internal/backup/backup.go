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
package backup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/execx"
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
}

// Engine runs backups for one configured server.
type Engine struct {
	cfg      *config.Config
	logger   *slog.Logger
	postgres Dumper // nil when Postgres is disabled
	restic   Backer
}

// New builds an Engine. runner is the execx.Runner used to construct the
// underlying Restic/PostgreSQL clients; logger defaults to slog.Default()
// if nil.
func New(cfg *config.Config, logger *slog.Logger, runner execx.Runner) (*Engine, error) {
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
	}
	if cfg.Postgres.Enabled {
		e.postgres = postgres.New(runner, cfg.Postgres)
	}
	return e, nil
}

// Run executes one backup end to end: acquire lock, (ping, dump, verify
// PostgreSQL if enabled), Restic backup, release lock. Every exit path --
// success, any failure, or ctx cancellation -- releases the lock and
// removes the local dump file via defer; see the failure/cleanup matrix in
// the design notes (docs/backup-flow.md).
//
// Run is safe to retry after any failure: a failed run leaves no partial
// state visible to Restic (the dump never reaches the repository unless
// verification passed), and re-running acquires a fresh lock. It is not
// idempotent in the sense of producing the same snapshot ID twice --
// Restic's own deduplication makes a content-identical rerun cheap and
// safe, not an error, but it is still a new snapshot.
func (e *Engine) Run(ctx context.Context) (Result, error) {
	start := time.Now()

	l, ok, err := lock.TryAcquire(e.cfg.Backup.LockFile)
	if err != nil {
		return Result{}, fmt.Errorf("backup: acquire lock: %w", err)
	}
	if !ok {
		return Result{}, lock.ErrLocked
	}
	defer l.Release()

	e.logger.Info("backup started", "operation", "backup", "host_tag", e.cfg.HostTag)

	var dumpPath string
	var dumpBytes int64

	if e.postgres != nil {
		if err := e.postgres.Ping(ctx); err != nil {
			return Result{}, fmt.Errorf("backup: postgres ping: %w", err)
		}

		dumpDir := filepath.Join(e.cfg.Backup.Root, "postgresql")
		if err := os.MkdirAll(dumpDir, 0o700); err != nil {
			return Result{}, fmt.Errorf("backup: create dump directory: %w", err)
		}

		meta, err := e.postgres.Dump(ctx, dumpDir)
		if meta.Path != "" {
			// Unconditional cleanup, registered as soon as a path
			// exists -- fires on success, any later failure, and
			// cancellation alike. The dump is never needed locally
			// once Restic has (or has not) backed it up.
			defer os.Remove(meta.Path)
		}
		if err != nil {
			return Result{}, fmt.Errorf("backup: dump: %w", err)
		}
		dumpPath = meta.Path
		dumpBytes = meta.Bytes

		if err := e.postgres.VerifyDump(ctx, dumpPath); err != nil {
			// Verification failed: Restic is never called. The dump
			// file is still removed by the defer above.
			return Result{}, fmt.Errorf("backup: verify dump: %w", err)
		}
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

	summary, err := e.restic.Backup(ctx, restic.BackupOptions{
		Paths:       paths,
		ExcludeFile: e.cfg.Backup.ExcludeFile,
		Tags:        tags,
		HostTag:     e.cfg.HostTag,
	})
	if err != nil {
		return Result{}, fmt.Errorf("backup: restic backup: %w", err)
	}

	result := Result{
		SnapshotID:   summary.SnapshotID,
		StartedAt:    start,
		FinishedAt:   time.Now(),
		Duration:     time.Since(start),
		DumpBytes:    dumpBytes,
		FilesNew:     summary.FilesNew,
		FilesChanged: summary.FilesChanged,
		Warnings:     summary.Warnings,
	}
	e.logger.Info("backup completed", "operation", "backup",
		"snapshot_id", result.SnapshotID, "duration", result.Duration, "warnings", len(result.Warnings))
	return result, nil
}
