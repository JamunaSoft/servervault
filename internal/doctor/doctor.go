// Package doctor implements ServerVault's non-destructive environment
// checks (`servervault doctor`). Every check reads state — configuration,
// the filesystem, PATH — and never writes or deletes anything.
//
// Checks that depend on the backup engine (internal/restic,
// internal/postgres, and friends — not yet implemented, see
// docs/architecture.md) report StatusSkip with a note on when they'll land,
// rather than being silently omitted or falsely reporting success.
package doctor

import (
	"context"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/execx"
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
	// depends on a package that doesn't exist yet — and is excluded from
	// pass/fail determination.
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

// Check is the result of one doctor check.
type Check struct {
	Name   string
	Status Status
	Detail string
}

// Report is the full set of checks from one doctor run.
type Report struct {
	Checks []Check
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

// Options configures a doctor run. Commands and FreeBytes are seams for
// tests; both default to real implementations when left nil.
type Options struct {
	Config    *config.Config
	Commands  execx.CommandChecker
	FreeBytes func(path string) (uint64, error)
}

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

	return Report{
		Checks: []Check{
			checkPlatform(),
			checkRequiredCommands(opts),
			checkConfigValidation(opts),
			checkSecretPermissions(opts),
			checkBackupPaths(opts),
			checkDiskSpace(opts),
			checkTimezone(),

			// Deferred until the backup engine (internal/restic,
			// internal/postgres, internal/lock) exists — see
			// docs/architecture.md's foundation/engine split and
			// ROADMAP.md's v0.3.0 milestone.
			deferredCheck("Restic repository access", "requires internal/restic (planned v0.3.0)"),
			deferredCheck("PostgreSQL connectivity", "requires internal/postgres (planned v0.3.0)"),
			deferredCheck("repository lock state", "requires internal/restic and internal/lock (planned v0.3.0)"),
			deferredCheck("SSH/SFTP non-interactive access", "requires repository-string parsing in internal/restic (planned v0.3.0)"),
			deferredCheck("systemd/timers", "requires the agent service (planned Phase 2)"),
		},
	}
}

func deferredCheck(name, reason string) Check {
	return Check{Name: name, Status: StatusSkip, Detail: reason}
}
