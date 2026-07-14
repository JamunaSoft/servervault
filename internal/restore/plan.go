// Package restore implements ServerVault's staging-first, temp-database-
// first restore: planning is separated from execution, and every plan is
// generated from real repository metadata (restic stats/ls), never
// guessed. See docs/restore-flow.md for the full flow, including Mermaid
// diagrams, and CLAUDE.md's non-negotiable rules 2-4 (never overwrite a
// live database, restore databases into a temporary DB, restore files
// into staging).
//
// This package is deliberately structured so no combination of inputs
// can produce a plan or an execution that targets a live path or the
// live database: Plan always generates a fresh destination (a new,
// randomly-suffixed staging directory, or a new, randomly-suffixed
// temporary database name derived from config), and Execute revalidates
// those destinations don't already exist immediately before writing
// anything -- see Executor.Execute.
package restore

import (
	"context"
	"time"

	"github.com/JamunaSoft/servervault/internal/restic"
)

// Target selects what a restore operation restores.
type Target string

const (
	// TargetFiles restores (a scope of) the snapshot's files into a
	// fresh staging directory.
	TargetFiles Target = "files"
	// TargetTempDB restores the snapshot's PostgreSQL dump into a fresh,
	// temporary database.
	TargetTempDB Target = "temp-db"
)

// ResticClient is the subset of *restic.Repository the restore engine
// needs -- read-only metadata queries (Stats, List) plus the one write
// operation (Restore). Consumers depend on this interface, not the
// concrete type, so orchestration logic is testable with a fake -- the
// same pattern internal/backup.Dumper/Backer already use.
type ResticClient interface {
	Stats(ctx context.Context, snapshotID string) (restic.Stats, error)
	List(ctx context.Context, snapshotID, path string) ([]restic.FileInfo, error)
	Restore(ctx context.Context, opts restic.RestoreOptions) (restic.RestoreSummary, error)
}

// PostgresClient is the subset of *postgres.Client the restore engine
// needs.
type PostgresClient interface {
	DatabaseExists(ctx context.Context, name string) (bool, error)
	CreateDatabase(ctx context.Context, name string) error
	DropDatabase(ctx context.Context, name string) error
	RestoreToTemp(ctx context.Context, dumpPath, databaseName string) error
	PingDatabase(ctx context.Context, name string) error
}

// Plan is an immutable description of one restore operation, generated
// entirely from real repository metadata and configuration -- never from
// guesses. Plan carries no credentials: RepositoryPath and Destination
// are plain filesystem/database-name strings, and nothing about
// connecting to the repository or database is stored here (that lives in
// the ResticClient/PostgresClient implementations the Planner and
// Executor were constructed with).
//
// Callers must treat a Plan as read-only. There is no exported method
// that mutates one.
type Plan struct {
	SnapshotID string
	Target     Target

	// RepositoryPath is the path scope within the snapshot this plan
	// restores: a user-requested --path for TargetFiles, or the
	// automatically located PostgreSQL dump file's path for
	// TargetTempDB. Empty means "the whole snapshot" (TargetFiles only).
	RepositoryPath string

	// Destination is where restic writes: a freshly generated staging
	// directory for TargetFiles, or an internal extraction directory
	// (holding just the extracted dump file before the PostgreSQL
	// restore step) for TargetTempDB.
	Destination string

	// TempDatabaseName is the freshly generated temporary database name,
	// set only for TargetTempDB. It is never equal to the live database
	// name -- see Planner.planTempDB.
	TempDatabaseName string

	// DatabaseName is the configured database this plan restores, set
	// only for TargetTempDB. In v0.4.0-alpha.1 there is exactly one
	// legal value (the configured postgres.database); this field and
	// PlanOptions.Database are the forward-compatible surface v0.5.0's
	// multi-database model extends without changing the interface.
	DatabaseName string

	ExpectedFiles int64
	ExpectedBytes int64
	// BytesKnown reports whether ExpectedBytes/ExpectedFiles were
	// actually measured against the repository (always true in this
	// milestone -- Plan never proceeds without a successful stats/list
	// query) as opposed to a placeholder zero value.
	BytesKnown bool

	RequiredCommands []string
	SafetyChecks     []string

	GeneratedAt time.Time
}

// PlanOptions configures Planner.Plan.
type PlanOptions struct {
	SnapshotID string
	Target     Target
	// Path scopes a TargetFiles restore to entries at or under this
	// repository path. Ignored for TargetTempDB (the dump file's path is
	// located automatically). Empty means the whole snapshot.
	Path string
	// Database selects which configured database to restore for
	// TargetTempDB. Empty defaults to the configured postgres.database;
	// if given, it must match that name (see ErrUnknownDatabase) -- see
	// Plan.DatabaseName's doc comment for why this flag already exists
	// ahead of v0.5.0's multi-database support.
	Database string
}
