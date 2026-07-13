// Package job defines ServerVault's typed job lifecycle: the states a
// backup, restore, or prune run passes through, which transitions between
// them are legal, and a SQLite-backed store that persists that history
// across process restarts.
//
// This package is deliberately narrow. It knows nothing about Cobra,
// HTTP, restic, or PostgreSQL -- it is pure state-machine and persistence
// logic, consumed by internal/restore today and, in a later milestone, by
// internal/backup and the local agent daemon. See docs/job-lifecycle.md
// for the full state diagram and docs/core-infrastructure.md for why this
// package exists as shared foundation rather than being built separately
// inside each consumer.
package job

import (
	"fmt"
)

// State is one point in a job's lifecycle.
type State string

const (
	StatePending     State = "pending"
	StatePreparing   State = "preparing"
	StateDumping     State = "dumping"
	StateBackingUp   State = "backing_up"
	StateVerifying   State = "verifying"
	StateCompleted   State = "completed"
	StateFailed      State = "failed"
	StateCancelled   State = "cancelled"
	StateInterrupted State = "interrupted"
)

// Terminal reports whether s is a final state: once reached, a job never
// transitions again. Completed, Failed, Cancelled, and Interrupted are all
// terminal -- an interrupted job is not resumed in place; a new job is
// created for a retry.
func (s State) Terminal() bool {
	switch s {
	case StateCompleted, StateFailed, StateCancelled, StateInterrupted:
		return true
	default:
		return false
	}
}

// Valid reports whether s is one of the known states.
func (s State) Valid() bool {
	switch s {
	case StatePending, StatePreparing, StateDumping, StateBackingUp, StateVerifying,
		StateCompleted, StateFailed, StateCancelled, StateInterrupted:
		return true
	default:
		return false
	}
}

// transitions maps each non-terminal state to the states it may legally
// move to next. Every non-terminal state can reach Cancelled (explicit
// user/operator cancellation) and Interrupted (reserved for the store's
// own reconciliation after an unclean restart -- see Store.Reconcile;
// application code should use Cancel, not transition to Interrupted
// directly). Dumping and BackingUp are both reachable from Preparing
// directly, since a restore job may skip straight to BackingUp (its
// "performing the restore" phase) without a Dumping phase, while a backup
// job with PostgreSQL enabled visits Dumping first.
//
// Verifying reaches both Completed and BackingUp: internal/restore's
// verification happens after the restore write (verifying -> completed
// only), while internal/backup's dump verification happens *before* the
// repository backup step -- pg_dump produces a dump, the dump is
// verified, and only then is it safe to hand to Restic (verifying ->
// backing_up). Both orderings are real, already-shipped control flow;
// the graph supports both rather than forcing one consumer's step order
// onto the other.
var transitions = map[State][]State{
	StatePending:   {StatePreparing, StateCancelled, StateInterrupted},
	StatePreparing: {StateDumping, StateBackingUp, StateVerifying, StateFailed, StateCancelled, StateInterrupted},
	StateDumping:   {StateBackingUp, StateVerifying, StateFailed, StateCancelled, StateInterrupted},
	StateBackingUp: {StateVerifying, StateCompleted, StateFailed, StateCancelled, StateInterrupted},
	StateVerifying: {StateCompleted, StateBackingUp, StateFailed, StateCancelled, StateInterrupted},
}

// CanTransition reports whether moving from `from` to `to` is a legal
// transition. A terminal `from` state can never transition again,
// regardless of `to`.
func CanTransition(from, to State) bool {
	if from.Terminal() {
		return false
	}
	for _, allowed := range transitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// TransitionError is returned when an illegal state transition is
// attempted, by CanTransition's callers (Store.Advance and the in-memory
// Job.Advance helper below).
type TransitionError struct {
	From State
	To   State
}

func (e *TransitionError) Error() string {
	if e.From.Terminal() {
		return fmt.Sprintf("job: %s is a terminal state and cannot transition to %s", e.From, e.To)
	}
	return fmt.Sprintf("job: illegal transition from %s to %s", e.From, e.To)
}

// Type identifies what kind of operation a job represents.
type Type string

const (
	TypeBackup  Type = "backup"
	TypeRestore Type = "restore"
	TypePrune   Type = "prune"
)

// ErrorCategory classifies why a job ended in StateFailed, StateCancelled,
// or StateInterrupted, without carrying the full (potentially sensitive)
// error text -- see Metadata's doc comment for the same reasoning applied
// to job metadata.
type ErrorCategory string

const (
	ErrorCategoryNone         ErrorCategory = ""
	ErrorCategoryLock         ErrorCategory = "lock"
	ErrorCategoryConnectivity ErrorCategory = "connectivity"
	ErrorCategoryValidation   ErrorCategory = "validation"
	ErrorCategoryExecution    ErrorCategory = "execution"
	ErrorCategoryCancelled    ErrorCategory = "cancelled"
	ErrorCategoryInterrupted  ErrorCategory = "interrupted"
	ErrorCategoryVerification ErrorCategory = "verification"
)

// Metadata carries safe, non-secret facts about a job. It is a closed set
// of typed fields, not a free-form map[string]string: there is
// deliberately no generic key/value setter anywhere in this package's
// public API, so a caller cannot accidentally attach a password, token, or
// credential-bearing URL to a persisted job record -- there is no field
// for one. This mirrors internal/restic's "the capability doesn't exist,
// not just unused" pattern (see that package's doc comment).
//
// If a future job type needs to record a new safe fact, add a new named
// field here -- do not add a generic map.
type Metadata struct {
	SnapshotID   string
	DatabaseName string
	PolicyName   string
	TargetPath   string
	HostTag      string
	BytesTotal   int64
	FilesNew     int
	FilesChanged int
}

// Job is one tracked unit of work.
type Job struct {
	ID            string
	Type          Type
	State         State
	Metadata      Metadata
	ErrorCategory ErrorCategory
	// ErrorSummary is a short, operator-facing description of a failure --
	// callers must not put secrets, full stderr, or credential-bearing
	// text here; it is persisted and later surfaced in status output and
	// events. Prefer ErrorCategory for programmatic handling.
	ErrorSummary string
}
