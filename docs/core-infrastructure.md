# Core infrastructure (v0.3.5)

`internal/job`, `internal/scheduler`, and `internal/event` are shared
foundation packages, not features in their own right. Nothing in
`servervault`'s CLI surface changes because of this milestone — see
[`ROADMAP.md`](../ROADMAP.md)'s v0.3.5 entry and each package's own doc
comment for the full rationale.

## Why build these now, and why these three specifically

A package earns a place in this early, shared layer only if at least
three later milestones would otherwise each build their own version of
it:

- **`internal/job`** — job history is needed by `internal/restore`
  (this session's second milestone), later by `internal/backup`, and
  later still by the local agent daemon's `JobHistory`. Building it once
  now means v0.9.0's agent daemon becomes a consumer of an
  already-proven package instead of inventing its own persistence format.
- **`internal/scheduler`** — schedule/retry math is needed by the local
  agent's `LocalScheduler` and, later, by the control plane's remote job
  queue. One implementation, two consumers, instead of two slightly
  different reimplementations of cron math and backoff.
- **`internal/event`** — structured operational history is needed by
  anything that wants to answer "what happened to job X" — starting with
  `internal/restore` in this session, and later the platform's audit log,
  which is designed to expose this same stream over the API rather than
  invent a parallel mechanism.

Two related packages were deliberately **not** pulled into this
milestone, even though they appear in the wider platform roadmap:

- **SSH** has no real caller yet. Nothing in ServerVault manages a
  concept of "another server" until the control plane exists (a much
  later milestone) — building an SSH package now would be abstracting
  over a use case that doesn't exist for many milestones.
- **A general storage abstraction** is unnecessary today: Restic already
  abstracts the storage backends directly (SFTP, S3-compatible, B2, a
  local path). ServerVault's own abstraction only earns its keep once
  there's more than one repository to manage centrally, which is a
  control-plane-era concern.

This mirrors the "shared infrastructure before duplication" principle
from the approved execution roadmap: build once, early, only what's
already known to have multiple real consumers; let everything else wait
for its first real caller.

## What each package is not

- **Not event sourcing.** `internal/event` is an append-only operational
  record — useful for "show me the history of job X" — not the source of
  truth application state is reconstructed from. `internal/job`'s SQLite
  rows remain authoritative for "what state is this job in right now."
- **Not a daemon.** `internal/scheduler` computes next-run times and
  backoff delays; it contains no ticker, no loop, and no goroutine that
  runs anything. A daemon that calls it on a timer is the local agent's
  job (v0.9.0), not this package's.
- **Not a general job queue.** `internal/job` tracks the lifecycle of
  jobs that already exist; it has no concept of dispatching work to a
  worker, prioritizing between jobs, or talking to a remote server. That
  is the control plane's job scheduler (a much later milestone), which is
  expected to reuse this package's state machine rather than replace it.

## Storage layout

Both `internal/job.Store` and `internal/event.Store` are SQLite
databases opened in WAL mode, with `MaxOpenConns` pinned to 1 (see each
package's `store.go` doc comment for why). They can point at the same
file — their migration bookkeeping tables (`schema_migrations` and
`event_schema_migrations`) are named distinctly so applying one package's
migrations never collides with the other's, even against a shared file
(see `internal/event/store_test.go`'s
`TestStore_SharesFileWithJobStore`).

Where that shared file lives on disk (a config field under something like
`agent.state_dir`) is a decision for v0.9.0's Local Agent milestone, once
there's a daemon process that actually owns a long-lived state directory
— this milestone only builds the packages, not their deployment location.

## Safety: no secrets in persisted state

Both `job.Metadata` and `event.Metadata` are closed, typed structs — a
fixed list of named fields (snapshot ID, database name, byte counts, and
so on) — not a generic `map[string]string`. There is no public
constructor or setter anywhere in either package that accepts an
arbitrary key/value pair. This is a structural guarantee, not a
convention: a caller cannot attach a password, token, or credential-
bearing URL to persisted job or event history, because there is no field
for one. Both packages also carry a reflection-based regression test
(`TestMetadata_NoSecretShapedFields`) that fails the build if a future
change ever adds a field whose name looks like it could hold a secret.

## See also

- [`docs/job-lifecycle.md`](job-lifecycle.md) — the job state machine
- [`docs/scheduler.md`](scheduler.md) — schedule and retry calculation
- [`docs/events.md`](events.md) — the event schema and sinks
- [`docs/testing.md`](testing.md) — how these packages are tested,
  including the real crash-consistency test
