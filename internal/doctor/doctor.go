// Package doctor implements ServerVault's non-destructive environment
// checks (`servervault doctor`). Every check reads state — configuration,
// the filesystem, PATH, the Restic repository, PostgreSQL — and never
// writes or deletes anything.
//
// Checks that still depend on functionality that doesn't exist yet (SSH/
// SFTP non-interactive reachability, systemd/timer integration) report
// StatusSkip with a note on when they'll land, rather than being silently
// omitted or falsely reporting success.
package doctor

import (
	"context"
	"time"

	"path/filepath"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/execx"
	"github.com/JamunaSoft/servervault/internal/health"
	"github.com/JamunaSoft/servervault/internal/job"
	"github.com/JamunaSoft/servervault/internal/postgres"
	"github.com/JamunaSoft/servervault/internal/restic"
)

// Status is the outcome of a single check.
type Status int

const (
	// StatusOK means the check passed.
	StatusOK Status = iota
	// StatusWarn flags something worth attention that does not fail the
	// overall run (e.g. a directory that doesn't exist yet but will be
	// created on first backup).
	StatusWarn
	// StatusFail means the check found a real problem; a Report
	// containing any StatusFail check is a failed doctor run.
	StatusFail
	// StatusSkip means the check could not run — typically because it
	// depends on functionality that doesn't exist yet, or isn't
	// configured — and is excluded from pass/fail determination.
	StatusSkip
)

// String returns the short label used in doctor's output.
func (s Status) String() string {
	switch s {
	case StatusOK:
		return "OK"
	case StatusWarn:
		return "WARN"
	case StatusFail:
		return "FAIL"
	case StatusSkip:
		return "SKIP"
	default:
		return "UNKNOWN"
	}
}

// MarshalJSON renders Status as its string label ("OK", "FAIL", ...)
// rather than the underlying int, so `servervault doctor --json` output
// is self-explanatory.
func (s Status) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

// Check is the result of one doctor check.
type Check struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
}

// Report is the full set of checks from one doctor run.
type Report struct {
	Checks []Check `json:"checks"`
}

// Failed reports whether any check in the report has StatusFail. StatusWarn
// and StatusSkip do not fail a doctor run — see CLAUDE.md's doctor exit
// code contract (0 all required checks pass, 1 one or more required checks
// fail).
func (r Report) Failed() bool {
	for _, c := range r.Checks {
		if c.Status == StatusFail {
			return true
		}
	}
	return false
}

// ResticAccessChecker is the subset of *restic.Repository doctor needs.
type ResticAccessChecker interface {
	CatConfig(ctx context.Context) error
}

// PostgresPinger is the subset of *postgres.Client doctor needs.
type PostgresPinger interface {
	Ping(ctx context.Context) error
}

// Options configures a doctor run. Commands, FreeBytes, Restic,
// Postgres, and Jobs are seams for tests; all default to real
// implementations built from Config when left nil. Jobs additionally
// defaults to nil (rather than a constructed *job.Store) if
// Config.StateDir can't be opened -- see Run's own comment on why that
// degrades to internal/health reporting StatusUnknown rather than
// failing the whole doctor run.
type Options struct {
	Config    *config.Config
	Commands  execx.CommandChecker
	FreeBytes func(path string) (uint64, error)
	Restic    ResticAccessChecker
	Postgres  PostgresPinger
	Jobs      health.JobLister
}

// networkCheckTimeout bounds how long the Restic/PostgreSQL reachability
// checks wait, so an unreachable repository or database doesn't hang the
// whole doctor run indefinitely.
const networkCheckTimeout = 10 * time.Second

// Run executes every check and returns a Report. It never returns an error
// itself — a check that cannot complete reports StatusFail or StatusSkip
// with an explanatory Detail instead.
func Run(ctx context.Context, opts Options) Report {
	if opts.Commands == nil {
		opts.Commands = execx.PathChecker{}
	}
	if opts.FreeBytes == nil {
		opts.FreeBytes = freeBytes
	}
	if opts.Restic == nil && opts.Config.Restic.Repository != "" {
		opts.Restic = restic.New(execx.DefaultRunner{}, opts.Config.Restic)
	}
	if opts.Postgres == nil && opts.Config.Postgres.Enabled {
		opts.Postgres = postgres.New(execx.DefaultRunner{}, opts.Config.Postgres)
	}
	if opts.Jobs == nil && opts.Config.StateDir != "" {
		// Best-effort, like Restic/Postgres above: a job store this run
		// opens itself is closed before Run returns. Opening (not
		// writing to) the store is read-only from a doctor run's
		// perspective -- LatestByType never creates a job record -- and
		// matches this package's existing "construct a real
		// implementation from Config when the caller didn't inject one"
		// convention. A failure to open (e.g. an unwritable state_dir)
		// leaves opts.Jobs nil, which internal/health already handles
		// by reporting StatusUnknown, not a doctor-run failure -- a
		// missing job history is not itself an environment problem
		// doctor exists to catch.
		if s, err := job.Open(filepath.Join(opts.Config.StateDir, "jobs.db")); err == nil {
			opts.Jobs = s
			defer s.Close()
		}
	}

	netCtx, cancel := context.WithTimeout(ctx, networkCheckTimeout)
	defer cancel()

	return Report{
		Checks: []Check{
			checkPlatform(),
			checkRequiredCommands(opts),
			checkConfigValidation(opts),
			checkSecretPermissions(opts),
			checkBackupPaths(opts),
			checkRestoreStagingOverlap(opts),
			checkDiskSpace(opts),
			checkTimezone(),
			checkResticAccess(netCtx, opts),
			checkPostgresConnectivity(netCtx, opts),
			checkLockState(opts),
			checkOperationalHealth(netCtx, opts),

			// Deferred: no code path exercises these yet.
			deferredCheck("SSH/SFTP non-interactive access", "requires repository-string parsing (planned when a dedicated SFTP check is scoped)"),
			deferredCheck("systemd/timers", "requires the agent service (planned Phase 2)"),
		},
	}
}

func deferredCheck(name, reason string) Check {
	return Check{Name: name, Status: StatusSkip, Detail: reason}
}
