// Package retention implements ServerVault's policy-driven snapshot
// retention ("servervault prune"): planning is separated from execution,
// exactly as internal/restore does, and every plan is generated from
// real repository metadata (a real `restic forget --dry-run`), never
// guessed. See docs/retention-flow.md for the full flow and CLAUDE.md's
// non-negotiable rule 7 ("refuse destructive cleanup if repository
// validation fails").
//
// This package is deliberately structured so pruning can never remove
// more than retention.max_delete_count snapshots or leave fewer than
// retention.min_keep_total remaining, and never runs at all against a
// repository that just failed a health check -- see Planner.Plan.
// Execute additionally revalidates the plan (re-running the same
// safety pipeline) immediately before the one irreversible call it
// makes, the same "critical assumptions checked again right before the
// first write" pattern internal/restore's Executor uses.
package retention

import (
	"context"
	"time"

	"github.com/JamunaSoft/servervault/internal/restic"
)

// ResticClient is the subset of *restic.Repository the retention engine
// needs -- read-only metadata queries (Snapshots, Check) plus the one
// write operation (Forget). Consumers depend on this interface, not the
// concrete type, matching internal/backup and internal/restore's
// existing fake-based testing pattern.
type ResticClient interface {
	Snapshots(ctx context.Context, opts restic.SnapshotsOptions) ([]restic.Snapshot, error)
	Check(ctx context.Context, opts restic.CheckOptions) error
	Forget(ctx context.Context, opts restic.ForgetOptions) (restic.ForgetSummary, error)
}

// Plan is an immutable description of one prune operation, generated
// entirely from real repository metadata -- never from guesses. A Plan
// with no error already reflects a repository that passed its health
// check and a removal set that already passed the minimum/maximum
// safety limits; Execute revalidates all of this again before acting on
// it, but a Plan the caller is about to show an operator (--dry-run) or
// ask them to confirm is already known-safe at the moment it was
// generated.
//
// Callers must treat a Plan as read-only. There is no exported method
// that mutates one.
type Plan struct {
	CurrentSnapshotCount int
	KeepSnapshotIDs      []string
	RemoveSnapshotIDs    []string
	RemoveCount          int
	// RemainingAfterPrune is CurrentSnapshotCount - RemoveCount --
	// already validated to be >= retention.min_keep_total.
	RemainingAfterPrune int

	SafetyChecks []string

	GeneratedAt time.Time
}
