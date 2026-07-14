package retention

import (
	"context"
	"fmt"
	"time"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/restic"
)

// Planner builds Plans from real repository metadata. It performs no
// destructive writes -- Check is read-only, and Forget is only ever
// called here with DryRun: true. Snapshots, Check, and a dry-run Forget
// are all restic calls that can safely be run at any time, including
// repeatedly for revalidation (see Executor.Execute).
type Planner struct {
	restic ResticClient
	cfg    *config.Config

	// now is overridden in tests for deterministic output; production
	// callers get NewPlanner's real-clock default.
	now func() time.Time
}

// NewPlanner builds a Planner. cfg must be non-nil and already validated
// (see config.Validate) -- Planner does not re-validate configuration
// structure, only the plan-specific safety checks documented on Plan.
func NewPlanner(resticClient ResticClient, cfg *config.Config) (*Planner, error) {
	if resticClient == nil {
		return nil, fmt.Errorf("retention: planner: restic client must not be nil")
	}
	if cfg == nil {
		return nil, fmt.Errorf("retention: planner: config must not be nil")
	}
	return &Planner{
		restic: resticClient,
		cfg:    cfg,
		now:    func() time.Time { return time.Now().UTC() },
	}, nil
}

// Plan queries the repository for its current snapshot set, validates
// repository health, computes what a real forget --prune would remove
// (via a dry run), and validates that removal set against the
// configured safety limits, in that order. Any failure at any step
// returns immediately -- a Plan is only ever returned once every check
// has passed.
func (p *Planner) Plan(ctx context.Context) (Plan, error) {
	generatedAt := p.now()
	var safetyChecks []string

	snapshots, err := p.restic.Snapshots(ctx, restic.SnapshotsOptions{Host: p.cfg.HostTag, Tags: p.tags()})
	if err != nil {
		return Plan{}, fmt.Errorf("retention: plan: list snapshots: %w", err)
	}
	currentCount := len(snapshots)

	if err := p.restic.Check(ctx, restic.CheckOptions{}); err != nil {
		return Plan{}, &ErrRepositoryUnhealthy{Err: err}
	}
	safetyChecks = append(safetyChecks, "repository health validated (restic check)")

	forgetSummary, err := p.restic.Forget(ctx, restic.ForgetOptions{
		Host:        p.cfg.HostTag,
		Tags:        p.tags(),
		KeepDaily:   p.cfg.Retention.KeepDaily,
		KeepWeekly:  p.cfg.Retention.KeepWeekly,
		KeepMonthly: p.cfg.Retention.KeepMonthly,
		DryRun:      true,
	})
	if err != nil {
		return Plan{}, fmt.Errorf("retention: plan: dry-run forget: %w", err)
	}

	removeCount := len(forgetSummary.RemovedSnapshotIDs)
	remaining := currentCount - removeCount

	if remaining < p.cfg.Retention.MinKeepTotal {
		return Plan{}, fmt.Errorf("retention: plan: %w (would leave %d, minimum is %d)",
			ErrBelowMinimumSnapshots, remaining, p.cfg.Retention.MinKeepTotal)
	}
	safetyChecks = append(safetyChecks, fmt.Sprintf(
		"remaining snapshot count after prune (%d) is at or above the configured minimum (%d)",
		remaining, p.cfg.Retention.MinKeepTotal))

	if removeCount > p.cfg.Retention.MaxDeleteCount {
		return Plan{}, &ErrMaxDeleteExceeded{PlannedCount: removeCount, MaxAllowed: p.cfg.Retention.MaxDeleteCount}
	}
	safetyChecks = append(safetyChecks, fmt.Sprintf(
		"planned deletion count (%d) is at or below the configured maximum (%d)",
		removeCount, p.cfg.Retention.MaxDeleteCount))

	return Plan{
		CurrentSnapshotCount: currentCount,
		KeepSnapshotIDs:      forgetSummary.KeptSnapshotIDs,
		RemoveSnapshotIDs:    forgetSummary.RemovedSnapshotIDs,
		RemoveCount:          removeCount,
		RemainingAfterPrune:  remaining,
		SafetyChecks:         safetyChecks,
		GeneratedAt:          generatedAt,
	}, nil
}

// tags returns the tag scope every Snapshots/Forget call in this package
// uses: "servervault" plus whatever additional tags are configured --
// the same convention internal/backup.Engine.Run uses when tagging a
// new snapshot, so retention only ever considers snapshots ServerVault
// itself created.
func (p *Planner) tags() []string {
	tags := make([]string, 0, len(p.cfg.Restic.Tags)+1)
	tags = append(tags, "servervault")
	tags = append(tags, p.cfg.Restic.Tags...)
	return tags
}
