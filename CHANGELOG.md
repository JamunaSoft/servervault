# Changelog

## Unreleased (go-rewrite, feature/core-infrastructure-v0.3.5)

Core infrastructure (v0.3.5 of the approved execution roadmap): shared
foundation packages, not user-facing features. No CLI behavior changed.

- `internal/job`: typed job lifecycle state machine (pending → preparing
  → dumping/backing_up → verifying → completed/failed/cancelled/
  interrupted), SQLite-backed (pure-Go `modernc.org/sqlite` driver, WAL
  mode, single pooled connection), optimistic-concurrency state
  transitions, and reconciliation of orphaned in-progress jobs to
  `interrupted` after an unclean restart -- verified with a real
  subprocess `SIGKILL` crash-consistency test, not a simulated one.
- `internal/scheduler`: hourly/daily/weekly next-run calculation with
  explicit, required IANA timezones (DST-correct, tested against a real
  `America/New_York` transition), explicit missed-run handling
  (skip vs. run-once), and bounded exponential backoff with injectable,
  deterministic-in-tests jitter. Pure calculation -- no daemon loop.
- `internal/event`: structured, append-only operational events, with a
  closed set of safe metadata fields (no free-form map) enforced by a
  reflection-based regression test, SQLite-backed sink plus no-op/
  in-memory sinks.
- New dependency: `modernc.org/sqlite` (pure Go, no cgo) -- a deliberate,
  documented addition, not incidental; see `docs/core-infrastructure.md`.
- Docs: `docs/core-infrastructure.md`, `docs/job-lifecycle.md`,
  `docs/scheduler.md`, `docs/events.md` (all with Mermaid diagrams),
  `docs/testing.md` updated.

### Completion pass: `internal/backup` integration

A follow-up pass completed the one acceptance criterion originally
deferred above: every `servervault backup` run now creates and tracks a
job record.

- `internal/backup.Engine`: every `Run` call creates a job and advances
  it through the typed lifecycle (`pending → preparing → [dumping →
  verifying] → backing_up → completed`, or to `failed`/`cancelled` on
  any other exit path), with structured events emitted at each phase.
  Job/event tracking is optional (`backup.WithJobStore`/
  `backup.WithEventSink`) and degrades safely -- a bookkeeping problem,
  whether missing configuration or a runtime failure, is logged and
  never blocks a backup. New, purely additive `Result.JobID` field.
- `internal/job`: `verifying` can now also transition to `backing_up`
  (additive graph edge) -- backup verifies its dump *before* the
  Restic write, the opposite order from restore's verify-after-write.
- `internal/job`/`internal/event`: fixed `Store.Open` to create its
  parent directory if missing (matching `internal/lock`'s established
  contract) -- found by wiring a real, writable `state_dir` through the
  CLI for the first time; every prior test happened to use a directory
  that already existed.
- New config field: `state_dir` (default `/var/lib/servervault`), where
  the job/event SQLite database lives.
- `servervault backup` now opens the job/event stores, reconciles any
  jobs left in progress by an unclean previous exit, and passes both
  into the engine -- with the same "never block a backup" degrade-safe
  policy applied at the CLI layer too.
- Tests: a table-driven suite covering success, dump failure,
  verification failure, restic failure, lock-busy, and cancellation,
  each checked against real job state and real event emissions; an
  end-to-end CLI test proving the whole chain (`job.Open` →
  `WithJobStore` → `Reconcile`) is wired together correctly, which is
  what caught the `Store.Open` directory bug above. All pre-existing
  `internal/backup` tests pass unmodified.

## 0.3.0-alpha - 2026-07-12 (go-rewrite)

Go rewrite foundation and backup engine (Phase A). Pre-release: not yet at
parity with the stable shell implementation on `main` — see
`ROADMAP.md`.

- Root CLI (`servervault version`, `doctor`, `config validate`, `backup`)
  with build metadata via `-ldflags`
- Layered YAML + environment configuration, with filesystem-free
  validation including path-overlap and deceptive-prefix safety checks
- Non-destructive `servervault doctor`, including real Restic/PostgreSQL/
  lock-state checks and `--json` output
- Backup engine: `internal/lock` (flock-based), `internal/restic`
  (backup/check/snapshots, exit-code and stderr-based error
  classification), `internal/postgres` (peer-auth dump/verify, no
  password anywhere), `internal/backup` (orchestration — lock, ping,
  dump, verify, backup, cleanup on every exit path)
- Structured logging (`log/slog`, text/JSON)
- Unit test suite (table-driven, fakes for all external commands) plus a
  separate real-binary integration test suite (`-tags=integration`),
  gated in CI: required Restic job, PostgreSQL job, and an opt-in
  real-lock-conflict probe (manual/scheduled only)
- CI: Go build/vet/test, Shell/ShellCheck, CodeQL, and Integration
  workflows, all green

Not yet implemented: MySQL/MariaDB, restore, retention/prune, health/
status, notifications. The Go backup/restore engine is not a drop-in
replacement for the shell implementation yet — `main` remains the
production-recommended branch until parity is reached.

## 0.1.0 - 2026-07-11

- Initial public MVP
- PostgreSQL custom-format dumps with Zstandard compression
- Restic backup, retention, verification, and safe restore
- SFTP repository configuration
- systemd services and timers
- ShellCheck workflow
