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
- Scope note: `internal/backup.Engine` was **not** modified to route
  through `internal/job`/`internal/event` in this milestone -- that
  retrofit of an already-shipped, already-tested package is deliberately
  deferred; see `docs/core-infrastructure.md` and `AI_MEMORY.md`.

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
