// Package event defines ServerVault's structured, append-only operational
// record: what happened, when, to which job, with what safe metadata.
// Events are consumed by internal/restore today and, in a later
// milestone, by internal/backup and the platform audit log (which exposes
// this same stream over the API rather than inventing its own event
// model -- see docs/core-infrastructure.md).
//
// This is explicitly not event sourcing: events are a record of what
// happened, not the source of truth application state is reconstructed
// from. internal/job's SQLite-backed Job rows remain authoritative for
// "what state is this job in right now" -- events are op­erational
// history alongside that, not instead of it.
//
// Events are also not a replacement for log/slog: slog is for
// operator-facing diagnostic text (see docs/logging in CLAUDE.md);
// events are a small, typed, queryable record meant to be listed,
// filtered, and displayed (e.g. "show me every event for job X"), kept
// deliberately separate so persisting an event never depends on how
// verbose the current log level happens to be.
package event

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Type identifies what kind of event occurred. New types are added here
// as new operations gain event coverage; this is a closed, typed set, not
// an arbitrary string, so a typo in a call site is a compile error, not a
// silently-unmatched event nobody ever queries for.
type Type string

const (
	TypeJobCreated Type = "job.created"
	TypeJobStarted Type = "job.started"

	TypeDatabaseDumpStarted   Type = "database_dump.started"
	TypeDatabaseDumpCompleted Type = "database_dump.completed"

	TypeBackupStarted   Type = "backup.started"
	TypeBackupCompleted Type = "backup.completed"

	TypeVerificationStarted   Type = "verification.started"
	TypeVerificationCompleted Type = "verification.completed"

	TypeRestorePlanned   Type = "restore.planned"
	TypeRestoreStarted   Type = "restore.started"
	TypeRestoreCompleted Type = "restore.completed"

	TypeJobFailed      Type = "job.failed"
	TypeJobCancelled   Type = "job.cancelled"
	TypeJobInterrupted Type = "job.interrupted"
)

// Severity is how significant an event is to an operator scanning
// history.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Metadata carries safe, non-secret facts about an event. Like
// internal/job.Metadata, this is a closed set of typed fields, not a
// free-form map[string]string -- there is no generic key/value setter
// anywhere in this package, so a caller cannot accidentally attach a
// password, token, or credential-bearing URL to a persisted event record.
// If a future event needs to record a new safe fact, add a named field
// here -- do not add a generic map. See TestMetadata_NoSecretShapedFields.
type Metadata struct {
	SnapshotID    string
	DatabaseName  string
	PolicyName    string
	TargetPath    string
	BytesTotal    int64
	FilesNew      int
	FilesChanged  int
	DurationMS    int64
	ErrorCategory string
	ErrorSummary  string
}

// Event is one structured, immutable record.
type Event struct {
	ID        string
	Type      Type
	Timestamp time.Time
	JobID     string
	// HostRef identifies the server/host this event pertains to, when
	// available (empty is valid -- not every context has one yet, e.g.
	// single-server CLI use before the platform's Server resource
	// exists).
	HostRef  string
	Severity Severity
	Metadata Metadata
}

// New constructs an Event with a generated ID and the current UTC time.
func New(typ Type, jobID string, severity Severity, meta Metadata) (Event, error) {
	id, err := newID()
	if err != nil {
		return Event{}, err
	}
	return Event{
		ID:        id,
		Type:      typ,
		Timestamp: time.Now().UTC(),
		JobID:     jobID,
		Severity:  severity,
		Metadata:  meta,
	}, nil
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
