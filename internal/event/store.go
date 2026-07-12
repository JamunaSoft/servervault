package event

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver; pure Go, no cgo
)

// migration is one forward-only schema change for the events table. See
// internal/job/migrations.go for the same pattern and rationale (no down
// migrations: this is disposable local operational history, not data
// worth a rollback path).
type migration struct {
	version int
	sql     string
}

var migrations = []migration{
	{
		version: 1,
		sql: `
CREATE TABLE events (
	id             TEXT PRIMARY KEY,
	type           TEXT NOT NULL,
	timestamp      TEXT NOT NULL,
	job_id         TEXT NOT NULL DEFAULT '',
	host_ref       TEXT NOT NULL DEFAULT '',
	severity       TEXT NOT NULL,
	snapshot_id    TEXT NOT NULL DEFAULT '',
	database_name  TEXT NOT NULL DEFAULT '',
	policy_name    TEXT NOT NULL DEFAULT '',
	target_path    TEXT NOT NULL DEFAULT '',
	bytes_total    INTEGER NOT NULL DEFAULT 0,
	files_new      INTEGER NOT NULL DEFAULT 0,
	files_changed  INTEGER NOT NULL DEFAULT 0,
	duration_ms    INTEGER NOT NULL DEFAULT 0,
	error_category TEXT NOT NULL DEFAULT '',
	error_summary  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_events_job_id ON events(job_id);
CREATE INDEX idx_events_type ON events(type);
CREATE INDEX idx_events_timestamp ON events(timestamp);
`,
	},
}

// Store is a SQLite-backed, append-only Sink: Emit inserts, and nothing in
// this package's public API ever updates or deletes an event row --
// there is no UpdateEvent or DeleteEvent method, matching the "append-
// only operational record" contract from the package doc comment.
//
// Store uses a table name and migration-tracking table
// (event_schema_migrations) distinct from internal/job's, so the two
// packages can safely point at the same underlying SQLite file (the
// design intent from docs/core-infrastructure.md: one local state file,
// not two) without their independent migration bookkeeping colliding.
type Store struct {
	db *sql.DB
}

// Open opens (creating if necessary) the SQLite database at path and
// applies any pending event-schema migrations. The caller must call Close
// when done. Safe to point at the same file path an internal/job.Store is
// also using -- WAL mode allows multiple connections to one SQLite file.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("event: store path must not be empty")
	}

	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("event: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("event: enable WAL mode: %w", err)
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
CREATE TABLE IF NOT EXISTS event_schema_migrations (
	version    INTEGER PRIMARY KEY,
	applied_at TEXT NOT NULL
);`); err != nil {
		return fmt.Errorf("event: create event_schema_migrations: %w", err)
	}

	applied := map[int]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT version FROM event_schema_migrations;`)
	if err != nil {
		return fmt.Errorf("event: read event_schema_migrations: %w", err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("event: scan event_schema_migrations: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("event: read event_schema_migrations: %w", err)
	}
	rows.Close()

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("event: begin migration %d: %w", m.version, err)
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("event: apply migration %d: %w", m.version, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO event_schema_migrations (version, applied_at) VALUES (?, ?);`,
			m.version, time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("event: record migration %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("event: commit migration %d: %w", m.version, err)
		}
	}
	return nil
}

// Emit implements Sink by inserting e as a new row. Emit never updates an
// existing row.
func (s *Store) Emit(ctx context.Context, e Event) error {
	if e.ID == "" {
		return errors.New("event: emit: ID must not be empty (use event.New to construct)")
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO events (
	id, type, timestamp, job_id, host_ref, severity,
	snapshot_id, database_name, policy_name, target_path,
	bytes_total, files_new, files_changed, duration_ms,
	error_category, error_summary
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`,
		e.ID, string(e.Type), e.Timestamp.UTC().Format(time.RFC3339Nano), e.JobID, e.HostRef, string(e.Severity),
		e.Metadata.SnapshotID, e.Metadata.DatabaseName, e.Metadata.PolicyName, e.Metadata.TargetPath,
		e.Metadata.BytesTotal, e.Metadata.FilesNew, e.Metadata.FilesChanged, e.Metadata.DurationMS,
		e.Metadata.ErrorCategory, e.Metadata.ErrorSummary,
	)
	if err != nil {
		return fmt.Errorf("event: emit %s: %w", e.ID, err)
	}
	return nil
}

// ByJob returns every event recorded for jobID, oldest first.
func (s *Store) ByJob(ctx context.Context, jobID string) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, type, timestamp, job_id, host_ref, severity,
       snapshot_id, database_name, policy_name, target_path,
       bytes_total, files_new, files_changed, duration_ms,
       error_category, error_summary
FROM events WHERE job_id = ? ORDER BY timestamp ASC, id ASC;`, jobID)
	if err != nil {
		return nil, fmt.Errorf("event: list for job %s: %w", jobID, err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var e Event
		var typ, sev, ts string
		if err := rows.Scan(&e.ID, &typ, &ts, &e.JobID, &e.HostRef, &sev,
			&e.Metadata.SnapshotID, &e.Metadata.DatabaseName, &e.Metadata.PolicyName, &e.Metadata.TargetPath,
			&e.Metadata.BytesTotal, &e.Metadata.FilesNew, &e.Metadata.FilesChanged, &e.Metadata.DurationMS,
			&e.Metadata.ErrorCategory, &e.Metadata.ErrorSummary,
		); err != nil {
			return nil, fmt.Errorf("event: scan: %w", err)
		}
		e.Type = Type(typ)
		e.Severity = Severity(sev)
		parsed, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return nil, fmt.Errorf("event: parse timestamp: %w", err)
		}
		e.Timestamp = parsed
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("event: list for job %s: %w", jobID, err)
	}
	return out, nil
}
