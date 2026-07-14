package restic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// ForgetOptions configures Forget.
type ForgetOptions struct {
	Host        string
	Tags        []string
	KeepDaily   int
	KeepWeekly  int
	KeepMonthly int
	// Prune requests that forgotten snapshots' now-unreferenced data be
	// physically removed from the repository immediately (`--prune`).
	// Without it, forget only removes the snapshots' index entries --
	// still irreversible (the snapshot can no longer be listed or
	// restored from), but the underlying data blobs are left for a
	// later prune to reclaim.
	Prune bool
	// DryRun requests `--dry-run`: forget reports what it would keep and
	// remove without changing the repository at all.
	DryRun bool
}

// ForgetSummary is the outcome of a Forget call: which snapshots restic
// decided to keep, and which it removed (or, under DryRun, would remove).
//
// Deliberately absent: bytes reclaimed. restic's `--prune` JSON output
// carries pruning statistics, but their exact shape was not verified
// against a real restic binary in the environment this code was written
// in (no restic installed there -- see internal/restic's other JSON-
// parsing methods for the same caveat). Rather than guess at a field
// that could silently misreport, this package reports only what it
// parses with confidence: the keep/remove snapshot ID lists from
// forget's well-documented `--json` group format, which is used
// identically whether or not --prune/--dry-run are set.
type ForgetSummary struct {
	KeptSnapshotIDs    []string
	RemovedSnapshotIDs []string
}

// Forget runs `restic forget --json [--dry-run] [--prune] --host <host>
// [--tag <tag>]... --keep-daily <n> --keep-weekly <n> --keep-monthly <n>`.
//
// This is the second deliberate, scoped exception to this package's
// "never delete a repository" design (after Restore in v0.4.0-alpha.1) --
// see the package doc comment. Forget can irreversibly remove snapshots;
// it enforces none of ServerVault's retention safety policy itself (
// minimum snapshot count, maximum deletion count, repository health
// validation) -- that lives in internal/retention's Planner/Executor,
// the same separation of concerns as internal/restore's Planner
// enforcing "never restore over live data" while this package's Restore
// method just does what it's told.
func (r *Repository) Forget(ctx context.Context, opts ForgetOptions) (ForgetSummary, error) {
	args := []string{"forget", "--json"}
	if opts.Host != "" {
		args = append(args, "--host", opts.Host)
	}
	for _, tag := range opts.Tags {
		args = append(args, "--tag", tag)
	}
	args = append(args,
		"--keep-daily", strconv.Itoa(opts.KeepDaily),
		"--keep-weekly", strconv.Itoa(opts.KeepWeekly),
		"--keep-monthly", strconv.Itoa(opts.KeepMonthly),
	)
	if opts.Prune {
		args = append(args, "--prune")
	}
	if opts.DryRun {
		args = append(args, "--dry-run")
	}

	stdout, stderr, runErr := r.run(ctx, args)
	if runErr != nil {
		return ForgetSummary{}, &ExitError{Code: classifyResult(runErr, stderr), Err: wrapWithStderr(runErr, "restic forget", stderr)}
	}

	summary, err := parseForgetJSON(stdout.Bytes())
	if err != nil {
		return ForgetSummary{}, fmt.Errorf("restic forget: parse output: %w", err)
	}
	return summary, nil
}

// forgetGroup is one entry of restic forget's `--json` output: a
// (host, tags) group along with the snapshots it decided to keep and
// remove. restic groups by the exact host+tag combination given on the
// command line, so a single-host, single-tag-set call like this
// package's produces at most one group in practice, but every group
// present is summed, not just the first, in case a future caller widens
// the filter.
type forgetGroup struct {
	Keep   []forgetSnapshot `json:"keep"`
	Remove []forgetSnapshot `json:"remove"`
}

type forgetSnapshot struct {
	ID      string `json:"id"`
	ShortID string `json:"short_id"`
}

// parseForgetJSON parses restic forget's `--json` output: a top-level
// JSON array of groups, present the same way whether or not
// --dry-run/--prune were given. An empty or unrecognized body is treated
// as a summary-free success (zero-value ForgetSummary) rather than an
// error -- forget having nothing to report (e.g. no snapshots matched at
// all) is a legitimate outcome, not a parse failure.
func parseForgetJSON(output []byte) (ForgetSummary, error) {
	trimmed := bytes.TrimSpace(output)
	if len(trimmed) == 0 {
		return ForgetSummary{}, nil
	}

	var groups []forgetGroup
	if err := json.Unmarshal(trimmed, &groups); err != nil {
		// Tolerate output this package doesn't recognize (e.g. a restic
		// version whose --json forget shape has changed) the same way
		// Restore's summary parsing does: forget itself already
		// succeeded (exit 0) by the time we're here, so a parse miss on
		// the summary shouldn't turn a successful run into an error --
		// it only means the caller gets an empty summary rather than
		// wrong information.
		return ForgetSummary{}, nil
	}

	var summary ForgetSummary
	for _, g := range groups {
		for _, s := range g.Keep {
			summary.KeptSnapshotIDs = append(summary.KeptSnapshotIDs, snapshotID(s))
		}
		for _, s := range g.Remove {
			summary.RemovedSnapshotIDs = append(summary.RemovedSnapshotIDs, snapshotID(s))
		}
	}
	return summary, nil
}

// snapshotID prefers the full ID; some restic versions omit "id" from
// forget's keep/remove entries and only populate "short_id".
func snapshotID(s forgetSnapshot) string {
	if s.ID != "" {
		return s.ID
	}
	return s.ShortID
}
