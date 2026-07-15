// Package health answers "is everything currently fine" -- a fast,
// repeatable operational pulse check, as distinct from
// internal/doctor's "is this environment correctly set up" one-time
// audit (config shape, secret permissions, required commands, and so
// on). It has no dependency on internal/doctor; doctor is free to
// depend on health (see its checkHealth), never the other way around.
//
// health.Run is the shared implementation behind both `servervault
// status` (its primary, native consumer) and one additional check
// folded into `servervault doctor` -- see ROADMAP.md's v0.5.0
// "internal/health checks wired into doctor and status".
package health

import (
	"context"
	"time"
)

// Status is the outcome of a single check.
type Status int

const (
	// StatusOK means the check found nothing to report.
	StatusOK Status = iota
	// StatusWarn flags something worth an operator's attention that
	// does not, by itself, mean anything is broken right now (e.g. a
	// lock currently held because a backup happens to be running, or a
	// last-successful-backup that's older than expected).
	StatusWarn
	// StatusFail means the check found a real, current problem.
	StatusFail
	// StatusUnknown means the check has nothing to report yet -- not a
	// problem, just missing data (e.g. no backup has ever run, or the
	// check wasn't given what it needs to run at all). Distinct from
	// internal/doctor's StatusSkip only in name, kept separate so
	// health's own Report/Failed semantics don't implicitly depend on
	// doctor's package.
	StatusUnknown
)

// String returns the short label used in status/doctor output.
func (s Status) String() string {
	switch s {
	case StatusOK:
		return "OK"
	case StatusWarn:
		return "WARN"
	case StatusFail:
		return "FAIL"
	case StatusUnknown:
		return "UNKNOWN"
	default:
		return "UNKNOWN"
	}
}

// MarshalJSON renders Status as its string label, matching
// internal/doctor.Status's identical convention.
func (s Status) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

// Check is the result of one health check.
type Check struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
}

// Report is the full set of checks from one health run.
type Report struct {
	Checks      []Check   `json:"checks"`
	GeneratedAt time.Time `json:"generated_at"`
}

// Failed reports whether any check has StatusFail. StatusWarn and
// StatusUnknown do not fail a report -- matching
// internal/doctor.Report.Failed's identical convention.
func (r Report) Failed() bool {
	for _, c := range r.Checks {
		if c.Status == StatusFail {
			return true
		}
	}
	return false
}

// ResticAccessChecker is the subset of *restic.Repository health needs.
// Defined independently of internal/doctor.ResticAccessChecker (an
// identically-shaped interface) rather than imported from it, so
// neither package depends on the other.
type ResticAccessChecker interface {
	CatConfig(ctx context.Context) error
}
