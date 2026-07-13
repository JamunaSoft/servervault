package job

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver; pure Go, no cgo
)

// ErrNotFound is returned by Get when no job exists with the given ID.
var ErrNotFound = errors.New("job: not found")

// ErrConcurrentUpdate is returned by Advance when another writer changed
// the job's state between this call's read and write -- the caller should
// re-read the job and decide whether to retry, rather than this package
// silently overwriting a concurrent update.
var ErrConcurrentUpdate = errors.New("job: concurrent update, retry")

// Store persists Job records to a local SQLite database. It is the only
// place in this package that touches a database; Job and the state-machine
// functions in job.go have no I/O of their own.
//
// SQLite is opened in WAL mode (crash-safe, standard journaling that
// survives an unclean process exit -- see docs/core-infrastructure.md) with
// MaxOpenConns pinned to 1. A single connection serializes every query
// through Go's database/sql connection pool instead of relying on SQLite's
// own busy-retry behavior across multiple connections: for a local,
// single-process job store (one CLI invocation or one agent process, never
// multiple processes sharing a file concurrently in this milestone) this
// is simpler and more predictable than tuning busy_timeout against real
// contention, and it is what makes Advance's compare-and-swap safe under
// `go test -race` without an additional in-process mutex.
type Store struct {
	db *sql.DB
}

// Open opens (creating if necessary) the SQLite database at path and
// applies any pending migrations. The caller must call Close when done.
// path's parent directory is created if missing (mode 0700, matching
// internal/lock's own TryAcquire, which has the same "the caller passed
// a path, not a pre-created directory" contract).
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("job: store path must not be empty")
	}

	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("job: create directory %s: %w", dir, err)
		}
	}

	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("job: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("job: enable WAL mode: %w", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("job: enable foreign keys: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version    INTEGER PRIMARY KEY,
	applied_at TEXT NOT NULL
);`); err != nil {
		return fmt.Errorf("job: create schema_migrations: %w", err)
	}

	applied := map[int]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT version FROM schema_migrations;`)
	if err != nil {
		return fmt.Errorf("job: read schema_migrations: %w", err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("job: scan schema_migrations: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("job: read schema_migrations: %w", err)
	}
	rows.Close()

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("job: begin migration %d: %w", m.version, err)
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("job: apply migration %d: %w", m.version, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?);`,
			m.version, time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("job: record migration %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("job: commit migration %d: %w", m.version, err)
		}
	}
	return nil
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("job: generate id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Create inserts a new job. If j.ID is empty, one is generated. If
// j.State is empty, it defaults to StatePending. CreatedAt/UpdatedAt are
// always set by Create, not by the caller.
func (s *Store) Create(ctx context.Context, j Job) (Job, error) {
	if j.ID == "" {
		id, err := newID()
		if err != nil {
			return Job{}, err
		}
		j.ID = id
	}
	if j.State == "" {
		j.State = StatePending
	}
	if !j.State.Valid() {
		return Job{}, fmt.Errorf("job: create: invalid initial state %q", j.State)
	}
	if j.Type == "" {
		return Job{}, errors.New("job: create: type must not be empty")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO jobs (
	id, type, state, created_at, updated_at,
	error_category, error_summary,
	snapshot_id, database_name, policy_name, target_path, host_tag,
	bytes_total, files_new, files_changed, row_version
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1);`,
		j.ID, string(j.Type), string(j.State), now, now,
		string(j.ErrorCategory), j.ErrorSummary,
		j.Metadata.SnapshotID, j.Metadata.DatabaseName, j.Metadata.PolicyName, j.Metadata.TargetPath, j.Metadata.HostTag,
		j.Metadata.BytesTotal, j.Metadata.FilesNew, j.Metadata.FilesChanged,
	)
	if err != nil {
		return Job{}, fmt.Errorf("job: create %s: %w", j.ID, err)
	}
	return j, nil
}

// Get returns the job with the given ID, or ErrNotFound.
func (s *Store) Get(ctx context.Context, id string) (Job, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, type, state, error_category, error_summary,
       snapshot_id, database_name, policy_name, target_path, host_tag,
       bytes_total, files_new, files_changed
FROM jobs WHERE id = ?;`, id)

	var j Job
	var jType, state, errCat string
	err := row.Scan(&j.ID, &jType, &state, &errCat, &j.ErrorSummary,
		&j.Metadata.SnapshotID, &j.Metadata.DatabaseName, &j.Metadata.PolicyName, &j.Metadata.TargetPath, &j.Metadata.HostTag,
		&j.Metadata.BytesTotal, &j.Metadata.FilesNew, &j.Metadata.FilesChanged,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("job: get %s: %w", id, err)
	}
	j.Type = Type(jType)
	j.State = State(state)
	j.ErrorCategory = ErrorCategory(errCat)
	return j, nil
}

// AdvanceOptions carries the additional fields Advance may update
// alongside a state transition.
type AdvanceOptions struct {
	// Metadata, if non-nil, replaces the job's stored metadata.
	Metadata      *Metadata
	ErrorCategory ErrorCategory
	ErrorSummary  string
}

// Advance transitions the job identified by id from its current state to
// `to`, validating the move with CanTransition first. It fails with
// *TransitionError if the transition is illegal (including when the
// current state is already terminal), ErrNotFound if the job doesn't
// exist, and ErrConcurrentUpdate if another writer changed the job's
// state between this call's read and write (a compare-and-swap on an
// internal row_version column, not just optimistic in name).
func (s *Store) Advance(ctx context.Context, id string, to State, opts AdvanceOptions) (Job, error) {
	if !to.Valid() {
		return Job{}, fmt.Errorf("job: advance %s: invalid target state %q", id, to)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, fmt.Errorf("job: advance %s: begin: %w", id, err)
	}
	defer tx.Rollback()

	var currentState string
	var rowVersion int64
	err = tx.QueryRowContext(ctx, `SELECT state, row_version FROM jobs WHERE id = ?;`, id).Scan(&currentState, &rowVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("job: advance %s: read current state: %w", id, err)
	}

	from := State(currentState)
	if !CanTransition(from, to) {
		return Job{}, &TransitionError{From: from, To: to}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	setStarted := from == StatePending
	setFinished := to.Terminal()

	query := `UPDATE jobs SET state = ?, updated_at = ?, row_version = row_version + 1`
	args := []any{string(to), now}

	if setStarted {
		query += `, started_at = ?`
		args = append(args, now)
	}
	if setFinished {
		query += `, finished_at = ?`
		args = append(args, now)
	}
	if opts.ErrorCategory != "" {
		query += `, error_category = ?`
		args = append(args, string(opts.ErrorCategory))
	}
	if opts.ErrorSummary != "" {
		query += `, error_summary = ?`
		args = append(args, opts.ErrorSummary)
	}
	if opts.Metadata != nil {
		query += `, snapshot_id = ?, database_name = ?, policy_name = ?, target_path = ?, host_tag = ?, bytes_total = ?, files_new = ?, files_changed = ?`
		m := opts.Metadata
		args = append(args, m.SnapshotID, m.DatabaseName, m.PolicyName, m.TargetPath, m.HostTag, m.BytesTotal, m.FilesNew, m.FilesChanged)
	}

	query += ` WHERE id = ? AND row_version = ?;`
	args = append(args, id, rowVersion)

	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return Job{}, fmt.Errorf("job: advance %s: %w", id, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Job{}, fmt.Errorf("job: advance %s: rows affected: %w", id, err)
	}
	if affected == 0 {
		// Someone else updated this row between our read and write.
		return Job{}, ErrConcurrentUpdate
	}

	if err := tx.Commit(); err != nil {
		return Job{}, fmt.Errorf("job: advance %s: commit: %w", id, err)
	}

	return s.Get(ctx, id)
}

// Reconcile marks every job left in a non-terminal state as
// StateInterrupted. It is intended to be called once, right after Open,
// by a process that owns this store's file: any job still pending,
// preparing, dumping, backing_up, or verifying at that point was left
// behind by a previous process incarnation that did not shut down
// cleanly (a crash, SIGKILL, or power loss) -- there is no other process
// still working on it, since this store's file is only ever written by
// one process at a time. It returns the number of jobs reconciled.
func (s *Store) Reconcile(ctx context.Context) (int, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
UPDATE jobs
SET state = ?, updated_at = ?, finished_at = ?, error_category = ?,
    row_version = row_version + 1
WHERE state NOT IN (?, ?, ?, ?);`,
		string(StateInterrupted), now, now, string(ErrorCategoryInterrupted),
		string(StateCompleted), string(StateFailed), string(StateCancelled), string(StateInterrupted),
	)
	if err != nil {
		return 0, fmt.Errorf("job: reconcile: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("job: reconcile: rows affected: %w", err)
	}
	return int(affected), nil
}
