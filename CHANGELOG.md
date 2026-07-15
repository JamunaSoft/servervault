# Changelog

## Unreleased (go-rewrite, feature/retention-v0.5.0)

Notifications, added in the same branch after retention landed (still
part of v0.5.0 of the approved execution roadmap):

- `internal/notify` (new package): `Notifier` interface, exactly
  matching the shape `docs/extensibility.md` sketched ahead of time.
  `WebhookNotifier` is the first-party implementation -- POSTs a small,
  secret-free JSON payload (fields copied directly from
  `event.Event`/`event.Metadata`'s own closed field set) to
  `notify.webhook_url`, bounded by a 10s timeout. `EventSink` wraps any
  existing `event.Sink`, notifying only on `event.TypeJobFailed` --
  never cancellation or interruption -- with zero changes to
  `internal/backup`, `internal/restore`, or `internal/retention`,
  which stay unaware it exists.
- `internal/cli`: `wrapEventSinkWithNotify` (shared by `backup`,
  `restore`, `prune`) wraps each command's real event store when
  `notify.enabled` is true.
- `config.Validate`: `notify.webhook_url` must be set and be an
  `http://`/`https://` URL when `notify.enabled` is true; no check when
  disabled.
- Tests: 10 unit tests in `internal/notify` (race-clean, 93.3%
  coverage) plus two CLI-level tests proving the wiring actually
  triggers a real HTTP POST end to end (and that it doesn't when
  disabled), using a real `httptest.Server`.
- Docs: `docs/notify.md`.

Retention (part of v0.5.0 of the approved execution roadmap):
`servervault prune`.

- `internal/restic`: added `Forget` -- the second deliberate, scoped
  write-capable addition after `Restore` (v0.4.0-alpha.1); `Init`/
  `Unlock` remain entirely absent. Reports only kept/removed snapshot
  IDs, parsed from restic's documented `--json` group format;
  deliberately does not parse `--prune`'s byte-reclaimed statistics,
  whose exact shape wasn't verified against a real restic binary in
  this environment.
- `internal/retention` (new package): `Planner.Plan` is read-only --
  lists snapshots, validates repository health (`restic check`),
  computes the removal set via a real `restic forget --dry-run`, then
  validates it against two new configurable safety limits
  (`retention.min_keep_total`, a hard floor Validate enforces at a
  minimum of 1 regardless of configured value; `retention.
  max_delete_count`, a hard ceiling with no "unlimited" value).
  `Executor.Execute` acquires a dedicated retention lock, refuses to
  run if a backup *or* a restore is in progress, recomputes and
  reconfirms the entire plan immediately before the one destructive
  call it ever makes (failing with a stale-plan error on any drift),
  and records every prune in `internal/job` history and
  `internal/event`.
- `internal/job`/`internal/event`: additive schema migration
  (`snapshots_removed` column) and a new `Metadata.SnapshotsRemoved`
  field on both; `internal/event` gains
  `TypeRetentionPlanned`/`Started`/`Completed`.
- New config fields: `retention.min_keep_total`, `retention.
  max_delete_count`, `retention.lock_file`.
- CLI: `servervault prune [--dry-run] [--yes] [--output text|json]`.
  Confirmation is required for a real (non-dry-run) prune, matching
  `servervault restore`'s exact confirmation model. Unlike the shell
  implementation (which runs `forget --prune` automatically at the end
  of every backup), this is a deliberate, separate, explicit command --
  see `docs/retention-flow.md`'s "Guiding rule" section.
- Integration tests build their own fixture (three real snapshots via a
  real `backup.Engine.Run`, landing in the same restic daily bucket for
  a deterministic, non-zero removal count under `keep_daily=1`):
  plan-never-writes, execute-removes-exactly-the-planned-set, both
  safety limits refusing without touching the repository, lock
  conflict, and cancellation. Runs in the existing `restic-integration`
  CI job (no PostgreSQL dependency, no workflow changes needed).
- Scope note: `internal/restic.Forget`'s byte-reclaimed statistics are
  not reported anywhere in this milestone -- see the `internal/restic`
  bullet above and `docs/retention-flow.md`'s "Known limitation"
  section.

## Unreleased (go-rewrite, feature/restore-v0.4.0-alpha.1)

Safe restore (v0.4.0-alpha.1 of the approved execution roadmap):
`servervault restore` and `servervault snapshots`.

- `internal/restic`: added `Restore`, `Stats`, `List` -- the one
  deliberate, scoped write-capable addition to a package that otherwise
  deliberately cannot delete a repository or restore over live data;
  `Init`/`Forget`/`Prune`/`Unlock` remain entirely absent.
- `internal/postgres`: added `RestoreToTemp` (decompress, validate via
  `pg_restore --list`, then restore into a caller-provided database
  name only), `CreateDatabase`/`DatabaseExists`/`DropDatabase`/
  `PingDatabase`, all rejecting database names outside a strict
  `[A-Za-z0-9_.-]` allow-list.
- `internal/restore` (new package): `Planner` builds an immutable `Plan`
  from real repository metadata (`restic stats`/`restic ls`) -- a
  `--dry-run` performs zero writes and its output is exactly what a real
  run would report, not an estimate. `Executor` acquires a dedicated
  restore lock, refuses to start while a backup is in progress,
  re-validates the plan's critical assumptions immediately before the
  first write, records every restore in `internal/job` history and
  `internal/event`, and cleans up on every exit path (a partially
  restored staging directory is marked `.incomplete` rather than
  deleted; a temporary database is dropped only if the same run created
  it).
- New config fields: `restore.lock_file` (concurrent-restore guard,
  separate from the backup lock) and `state_dir` (where the job/event
  SQLite database lives, default `/var/lib/servervault`).
- CLI: `servervault snapshots [--json]`,
  `servervault restore --snapshot <id> --target files|temp-db [--path]
  [--database] [--dry-run] [--yes] [--output text|json]`. Confirmation
  is required for a real (non-dry-run) restore: `--yes`, or typing
  `yes` at an interactive prompt -- a non-interactive caller with
  neither is treated as not confirmed, never as confirmed by default.
- Integration tests build their own fixture by running a real
  `backup.Engine.Run` first, then restore from that real snapshot for
  real: file restore success/dry-run/existing-destination-rejection/
  invalid-snapshot/cancellation, and temp-database restore success
  (verifying the live database is untouched)/name-collision-revalidation/
  missing-dump-rejection. `postgres-integration` CI now installs restic
  alongside PostgreSQL, since these are the only tests needing both
  real binaries in the same job.
- Scope note: `internal/restic`'s restore-summary JSON field parsing was
  not verified against a real restic binary in the sandboxed environment
  this was written in (restic wasn't installed there) -- only against
  fixture JSON matching restic's documented output schema. CI's real
  `restic`-installed jobs are the first actual verification of that
  parsing; see AI_MEMORY.md.

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
