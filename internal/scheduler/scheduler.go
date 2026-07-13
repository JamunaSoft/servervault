// Package scheduler computes when a named, recurring job should next run
// and how to react when it didn't -- pure calculation, no daemon loop and
// no third-party scheduling library. It is consumed by the local agent
// daemon (a later milestone) and, indirectly, by internal/policy's
// schedule field; this package itself knows nothing about jobs, policies,
// or backups.
//
// Schedules are expressed as a small, explicit structure (frequency,
// time-of-day, weekday, IANA location) rather than a general cron
// expression. A hand-rolled cron parser is real correctness risk --
// especially around DST -- for a feature set the roadmap only ever needs
// "daily/weekly/hourly at a given wall-clock time"; a full cron grammar
// would be speculative generality with no current consumer (see
// docs/core-infrastructure.md). If a genuine need for arbitrary cron
// expressions appears later, that is its own scoped addition, not a
// retrofit of this type.
package scheduler

import (
	"fmt"
	"time"
)

// Frequency is how often a Schedule recurs.
type Frequency string

const (
	FrequencyHourly Frequency = "hourly"
	FrequencyDaily  Frequency = "daily"
	FrequencyWeekly Frequency = "weekly"
)

// Schedule describes when a named job should next run. Location is
// required (never implicitly "local time") so a schedule's meaning does
// not silently change if the process running it moves to a host with a
// different system timezone -- see docs/scheduler.md.
type Schedule struct {
	Name      string
	Frequency Frequency

	// At is the wall-clock time of day the job should run, "HH:MM" in
	// 24-hour format. Required for Daily and Weekly; ignored for Hourly
	// (an hourly schedule runs at :00 of every hour).
	At string

	// Weekday selects the day for Weekly schedules; ignored otherwise.
	Weekday time.Weekday

	// Location is the IANA timezone the schedule's wall-clock time is
	// interpreted in. Must not be nil -- see Validate.
	Location *time.Location
}

// Validate reports whether s is well-formed: known frequency, parseable
// At (where required), non-nil Location.
func (s Schedule) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("scheduler: schedule name must not be empty")
	}
	if s.Location == nil {
		return fmt.Errorf("scheduler: schedule %q: location must not be nil", s.Name)
	}
	switch s.Frequency {
	case FrequencyHourly:
		return nil
	case FrequencyDaily, FrequencyWeekly:
		if _, _, err := parseClock(s.At); err != nil {
			return fmt.Errorf("scheduler: schedule %q: %w", s.Name, err)
		}
		return nil
	default:
		return fmt.Errorf("scheduler: schedule %q: unknown frequency %q", s.Name, s.Frequency)
	}
}

func parseClock(at string) (hour, minute int, err error) {
	if _, err := fmt.Sscanf(at, "%d:%d", &hour, &minute); err != nil {
		return 0, 0, fmt.Errorf("invalid time-of-day %q, want HH:MM: %w", at, err)
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid time-of-day %q: hour must be 0-23 and minute 0-59", at)
	}
	return hour, minute, nil
}

// NextRun returns the first occurrence of s strictly after `after`, in
// s.Location. Go's time.Date already resolves the DST edge cases that
// would otherwise make this fiddly: a wall-clock time that doesn't exist
// (spring-forward gap) or is ambiguous (fall-back overlap) is normalized
// according to Go's documented, deterministic rule rather than panicking
// or silently picking an arbitrary offset -- see scheduler_test.go for
// explicit coverage across a real DST transition.
func (s Schedule) NextRun(after time.Time) (time.Time, error) {
	if err := s.Validate(); err != nil {
		return time.Time{}, err
	}
	after = after.In(s.Location)

	switch s.Frequency {
	case FrequencyHourly:
		next := time.Date(after.Year(), after.Month(), after.Day(), after.Hour(), 0, 0, 0, s.Location)
		if !next.After(after) {
			next = next.Add(time.Hour)
		}
		return next, nil

	case FrequencyDaily:
		hour, minute, _ := parseClock(s.At)
		next := time.Date(after.Year(), after.Month(), after.Day(), hour, minute, 0, 0, s.Location)
		if !next.After(after) {
			next = next.AddDate(0, 0, 1)
		}
		return next, nil

	case FrequencyWeekly:
		hour, minute, _ := parseClock(s.At)
		next := time.Date(after.Year(), after.Month(), after.Day(), hour, minute, 0, 0, s.Location)
		daysUntil := (int(s.Weekday) - int(next.Weekday()) + 7) % 7
		next = next.AddDate(0, 0, daysUntil)
		if !next.After(after) {
			next = next.AddDate(0, 0, 7)
		}
		return next, nil

	default:
		return time.Time{}, fmt.Errorf("scheduler: schedule %q: unknown frequency %q", s.Name, s.Frequency)
	}
}

// MissedRunPolicy is how CatchUp reacts when one or more scheduled
// occurrences passed with the job never having run (the agent was
// stopped, the host was down, etc.) -- see docs/scheduler.md. There is
// deliberately no implicit default baked into NextRun itself: a caller
// must pick one.
type MissedRunPolicy string

const (
	// MissedRunSkip ignores any occurrences between lastRun and now and
	// only reports the next future occurrence -- the common default for
	// routine backups, where running three days of missed backups back
	// to back is rarely useful.
	MissedRunSkip MissedRunPolicy = "skip"
	// MissedRunRunOnce reports that the job should run immediately (once,
	// not once per missed occurrence) if at least one occurrence was
	// missed, then resumes the normal schedule from there.
	MissedRunRunOnce MissedRunPolicy = "run_once"
)

// CatchUpResult is the outcome of applying a MissedRunPolicy.
type CatchUpResult struct {
	// Missed is how many scheduled occurrences fell between lastRun and
	// now without the job having run.
	Missed int
	// RunNow reports whether the caller should run the job immediately,
	// rather than waiting for NextRun.
	RunNow bool
	// NextRun is when the job should next run if not run now (or, when
	// RunNow is true, when it should run *after* this catch-up run).
	NextRun time.Time
}

// CatchUp counts how many occurrences of s fell between lastRun
// (exclusive) and now (inclusive), and applies policy to decide whether
// the caller should run immediately.
func (s Schedule) CatchUp(lastRun, now time.Time, policy MissedRunPolicy) (CatchUpResult, error) {
	if err := s.Validate(); err != nil {
		return CatchUpResult{}, err
	}

	missed := 0
	cursor := lastRun
	for {
		next, err := s.NextRun(cursor)
		if err != nil {
			return CatchUpResult{}, err
		}
		if next.After(now) {
			break
		}
		missed++
		cursor = next
		if missed > 100000 {
			// Defensive bound: a caller passing a wildly stale lastRun
			// (years ago) should get a fast, clear answer, not an
			// effectively unbounded loop.
			return CatchUpResult{}, fmt.Errorf("scheduler: schedule %q: too many missed occurrences to count (lastRun too far in the past)", s.Name)
		}
	}

	next, err := s.NextRun(now)
	if err != nil {
		return CatchUpResult{}, err
	}

	result := CatchUpResult{Missed: missed, NextRun: next}
	if missed > 0 && policy == MissedRunRunOnce {
		result.RunNow = true
	}
	return result, nil
}

// ConcurrencyLimit bounds how many instances of a schedule (or a group of
// schedules sharing a limit) may run at once. It is a representable value
// only in this milestone -- enforcing it against real running jobs is the
// local agent daemon's responsibility (a later milestone), not this
// package's; see docs/core-infrastructure.md.
type ConcurrencyLimit struct {
	// Max is the maximum number of concurrently running instances. Zero
	// means unlimited.
	Max int
}

// Allow reports whether one more instance may start, given current
// already-running instances.
func (c ConcurrencyLimit) Allow(current int) bool {
	if c.Max <= 0 {
		return true
	}
	return current < c.Max
}
