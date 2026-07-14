package retention

import (
	"errors"
	"fmt"
)

// ErrBelowMinimumSnapshots is returned by Plan when the computed removal
// set would leave fewer snapshots than retention.min_keep_total -- the
// hard floor config.Validate enforces at a minimum of 1. This is
// defense in depth beyond the keep_daily/weekly/monthly policy itself:
// a keep policy that resolves to "keep almost nothing" for an unusual
// snapshot history is caught here rather than silently pruning down to
// (or near) zero.
var ErrBelowMinimumSnapshots = errors.New("retention: pruning would leave fewer snapshots than the configured minimum")

// ErrBackupInProgress is returned by Execute when a backup currently
// holds its lock. Retention does not queue or wait -- it fails fast, the
// same choice internal/restore already makes for the same reason (see
// that package's ErrBackupInProgress doc comment).
var ErrBackupInProgress = errors.New("retention: a backup is currently running; wait for it to complete before pruning")

// ErrRestoreInProgress is returned by Execute when a restore currently
// holds its lock. Retention checks this in addition to the backup lock
// (internal/restore only checks the backup lock) because forget --prune
// is the most destructive of the three operations: a restore reading
// from the repository while prune rewrites/removes pack files is a
// scenario worth refusing outright rather than relying solely on
// restic's own exclusive-lock behavior to prevent it.
var ErrRestoreInProgress = errors.New("retention: a restore is currently running; wait for it to complete before pruning")

// ErrRepositoryUnhealthy is returned by Plan when `restic check` fails.
// CLAUDE.md's non-negotiable rule 7: "refuse destructive cleanup if
// repository validation fails" -- Plan refuses to even compute a forget
// plan against a repository that hasn't passed a health check, let alone
// Execute perform one.
type ErrRepositoryUnhealthy struct {
	Err error
}

func (e *ErrRepositoryUnhealthy) Error() string {
	return fmt.Sprintf("retention: repository failed health validation; refusing to prune: %v", e.Err)
}
func (e *ErrRepositoryUnhealthy) Unwrap() error { return e.Err }

// ErrMaxDeleteExceeded is returned by Plan when the computed removal
// count exceeds retention.max_delete_count. There is no way to bypass
// this from a single prune run -- it exists specifically to turn a
// catastrophic keep-policy misconfiguration into a refused run with a
// clear, actionable error instead of a silent mass deletion.
type ErrMaxDeleteExceeded struct {
	PlannedCount int
	MaxAllowed   int
}

func (e *ErrMaxDeleteExceeded) Error() string {
	return fmt.Sprintf("retention: planned deletion of %d snapshot(s) exceeds the configured maximum of %d", e.PlannedCount, e.MaxAllowed)
}

// ErrPlanStale is returned by Execute when the revalidated plan
// (recomputed immediately before the destructive forget --prune call)
// disagrees with the Plan passed in -- e.g. a backup created a new
// snapshot, or another prune already ran, between planning and
// execution. Mirrors internal/restore's identically-named,
// identically-reasoned type.
type ErrPlanStale struct {
	Reason string
}

func (e *ErrPlanStale) Error() string {
	return fmt.Sprintf("retention: plan is no longer valid: %s", e.Reason)
}
