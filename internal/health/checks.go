package health

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/job"
	"github.com/JamunaSoft/servervault/internal/lock"
)

// JobLister is the subset of *job.Store health needs.
type JobLister interface {
	LatestByType(ctx context.Context, t job.Type) (job.Job, error)
}

// defaultStaleAfter is how old a last-*successful*-backup can be before
// checkLastJob(job.TypeBackup) warns. Only applied to backup -- restore
// and prune have no expected regular cadence, so their checks report
// the last outcome without a freshness opinion.
const defaultStaleAfter = 48 * time.Hour

// Options configures a health run. Restic and Jobs are seams for
// tests; both are safe to leave nil (checks that need them report
// StatusUnknown with an explanatory Detail rather than panicking or
// silently skipping). Now/StaleAfter default to the real clock and
// defaultStaleAfter.
type Options struct {
	Config     *config.Config
	Restic     ResticAccessChecker
	Jobs       JobLister
	Now        func() time.Time
	StaleAfter time.Duration
}

// Run executes every check and returns a Report. It never returns an
// error itself -- a check that cannot complete reports StatusFail or
// StatusUnknown with an explanatory Detail instead, the same contract
// internal/doctor.Run already established.
func Run(ctx context.Context, opts Options) Report {
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	if opts.StaleAfter == 0 {
		opts.StaleAfter = defaultStaleAfter
	}

	checks := []Check{
		checkResticAccess(ctx, opts),
	}

	if opts.Config != nil {
		checks = append(checks,
			checkLockState("backup", opts.Config.Backup.LockFile),
			checkLockState("restore", opts.Config.Restore.LockFile),
			checkLockState("retention", opts.Config.Retention.LockFile),
		)
	}

	checks = append(checks,
		checkLastJob(ctx, opts, job.TypeBackup, "last backup", true),
		checkLastJob(ctx, opts, job.TypeRestore, "last restore", false),
		checkLastJob(ctx, opts, job.TypePrune, "last prune", false),
	)

	return Report{Checks: checks, GeneratedAt: opts.Now()}
}

// checkResticAccess performs the same cheapest-possible reachability
// check internal/doctor's own checkResticAccess does (`restic cat
// config`) -- intentionally duplicated rather than shared, since
// sharing it would require doctor and health to depend on each other
// or on a third package, for five lines of logic neither package's
// tests have ever found a reason to change independently.
func checkResticAccess(ctx context.Context, opts Options) Check {
	const name = "repository reachability"

	if opts.Config == nil || opts.Config.Restic.Repository == "" {
		return Check{Name: name, Status: StatusUnknown, Detail: "restic.repository is not configured"}
	}
	if opts.Restic == nil {
		return Check{Name: name, Status: StatusUnknown, Detail: "no Restic client available"}
	}
	if err := opts.Restic.CatConfig(ctx); err != nil {
		return Check{Name: name, Status: StatusFail, Detail: err.Error()}
	}
	return Check{Name: name, Status: StatusOK, Detail: "repository reachable and password valid"}
}

// checkLockState reports whether label's operation is currently
// running, via internal/lock's non-destructive Status probe. A held
// lock is StatusWarn, not StatusFail -- it usually just means that
// operation is legitimately running right now. Unlike
// internal/doctor.checkLockState (which only ever checked the backup
// lock), this covers all three: backup, restore, and retention.
func checkLockState(label, path string) Check {
	name := label + " lock state"
	if path == "" {
		return Check{Name: name, Status: StatusUnknown, Detail: label + "'s lock file is not configured"}
	}

	held, detail, err := lock.Status(path)
	if err != nil {
		return Check{Name: name, Status: StatusFail, Detail: err.Error()}
	}
	if held {
		return Check{Name: name, Status: StatusWarn, Detail: fmt.Sprintf("%s is currently running (%s)", label, strings.ReplaceAll(strings.TrimSpace(detail), "\n", ", "))}
	}
	return Check{Name: name, Status: StatusOK, Detail: "not currently running"}
}

// checkLastJob reports the outcome and age of the most recently
// created job of type t. checkFreshness, when true, additionally warns
// if the last *completed* job is older than opts.StaleAfter -- used for
// backup (which has an expected regular cadence) and not for restore/
// prune (which don't).
func checkLastJob(ctx context.Context, opts Options, t job.Type, name string, checkFreshness bool) Check {
	if opts.Jobs == nil {
		return Check{Name: name, Status: StatusUnknown, Detail: "no job history available"}
	}

	j, err := opts.Jobs.LatestByType(ctx, t)
	if errors.Is(err, job.ErrNotFound) {
		return Check{Name: name, Status: StatusUnknown, Detail: fmt.Sprintf("no %s has ever run", t)}
	}
	if err != nil {
		return Check{Name: name, Status: StatusFail, Detail: err.Error()}
	}

	if !j.State.Terminal() {
		return Check{Name: name, Status: StatusOK, Detail: fmt.Sprintf("currently %s", j.State)}
	}

	age := opts.Now().Sub(j.FinishedAt).Round(time.Second)

	switch j.State {
	case job.StateCompleted:
		if checkFreshness && age > opts.StaleAfter {
			return Check{Name: name, Status: StatusWarn, Detail: fmt.Sprintf("last completed %s ago -- older than the %s freshness threshold", age, opts.StaleAfter)}
		}
		return Check{Name: name, Status: StatusOK, Detail: fmt.Sprintf("completed %s ago", age)}
	case job.StateFailed:
		return Check{Name: name, Status: StatusFail, Detail: fmt.Sprintf("failed %s ago: %s", age, j.ErrorSummary)}
	case job.StateCancelled:
		return Check{Name: name, Status: StatusWarn, Detail: fmt.Sprintf("cancelled %s ago", age)}
	case job.StateInterrupted:
		return Check{Name: name, Status: StatusWarn, Detail: fmt.Sprintf("interrupted %s ago (the process did not shut down cleanly)", age)}
	default:
		return Check{Name: name, Status: StatusUnknown, Detail: fmt.Sprintf("unrecognized terminal state %q", j.State)}
	}
}
