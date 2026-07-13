# ServerVault Roadmap

This roadmap tracks the path from the stable shell implementation
(`main`) to a complete Go rewrite (`go-rewrite`). See
[`PROJECT_STATUS.md`](PROJECT_STATUS.md) for the current detailed status.

## v0.1.x — Stable shell implementation (`main`)

Status: released.

- PostgreSQL custom-format dumps with Zstandard compression
- Restic backup, retention, and verification
- Website/server config backup
- Safe, menu-driven restore tooling
- systemd services and timers
- ShellCheck CI

This branch receives bug fixes only. New features go into the Go
rewrite.

## v0.2.0-alpha — Go foundation (`go-rewrite`)

Status: foundation complete.

- [x] Cobra root CLI and `servervault version`
- [x] Config package: defaults, YAML, environment overrides (`internal/config`) — flags are limited to `--config` for now; full per-field flag overrides are a documented future addition, not required for the foundation milestone
- [x] `servervault config validate`
- [x] Structured logging with `log/slog` (`internal/logger`)
- [x] `servervault doctor` (non-destructive environment checks) — checks that depend on the backup engine (Restic repository access, PostgreSQL connectivity, repository lock state, SSH/SFTP reachability, systemd/timers) report `SKIP` with a note on when they land, rather than being faked; see `internal/doctor`
- [x] Build metadata via `-ldflags` (`internal/version`)
- [x] Unit test coverage for config, doctor, logger, execx, version, and cli packages
- [x] Go CI (`gofmt`, `go vet`, `go test`, `go build`)

The Go backup/restore engine is intentionally **not** started until this
foundation is stable — see the "Current priority" section of
[`CLAUDE.md`](CLAUDE.md). Next: v0.3.0 below.

## v0.3.0 — Go backup engine

Status: Phase A complete (Restic + PostgreSQL); MySQL not started.

- [x] `internal/restic` wrapper (context-aware, cancellable) — `Backup`,
      `Check`, `Snapshots`, `CatConfig`; exit codes classified (0/1/2/3/
      10/11/12/130). Deliberately no `Init`/`Forget`/`Prune`/`Restore`/
      `Unlock` — those capabilities don't exist in the package, not just
      unused.
- [x] `internal/postgres` dump/verify — peer-auth only (`sudo -u <user>`
      over the local Unix socket, matching the shell implementation; no
      password anywhere), `pg_dump | zstd` piped and streamed to a 0600
      temp file, `VerifyDump` always cleans up its temp file even on
      failure.
- [ ] `internal/mysql` dump/verify (MySQL/MariaDB — scope addition beyond
      the original `CLAUDE.md` package list, needed for the platform's
      stated multi-database requirement) — not started
- [x] `internal/backup` orchestration — lock → ping → dump → verify →
      Restic backup → cleanup; a failed verification never reaches Restic
      (tested explicitly); every exit path cleans up the dump file and
      releases the lock
- [x] `internal/lock` to prevent concurrent runs — `flock`-based, kernel-
      managed, no PID-file staleness; same lock path as the shell
      implementation on purpose (see `docs/security-model.md`)
- [x] `servervault backup`
- [x] `servervault doctor` gained real Restic/PostgreSQL/lock-state checks
      (previously `SKIP`), plus `--json` output
- [x] Integration test milestone — real `restic`/`pg_dump`/`pg_restore`/
      `psql` against temporary local repositories and disposable
      databases, gated behind a dedicated `integration` build tag,
      separate from the default unit suite. Covers: real backup/check/
      snapshots/wrong-password (Restic), real dump/verify/corrupted-dump
      (PostgreSQL), end-to-end `Engine.Run` with PostgreSQL on/off,
      cancellation, and the full cleanup matrix against real subprocesses.
      A concurrent-lock test (real `flock`, fake backends) runs
      untagged, always. A restic lock-*conflict* probe against a real
      backend is opt-in (`resticlock` tag), version-sensitive, and
      manual/scheduled-only in CI — the deterministic version of that
      check (exit code 11 classification) is a normal unit test. See
      `.github/workflows/integration.yml`, `docs/testing.md`.

Not in Phase A (later v0.3.0 work or v0.4.0+): retention/prune, restore,
notifications, `internal/mysql`. See `AI_MEMORY.md` for the full Phase A
design record (interfaces, error taxonomy, failure/cleanup matrix).

## v0.3.5 — Core infrastructure

Status: complete, including the `internal/backup` integration originally
deferred (see below).

Shared foundation packages, not user-facing features -- see
[`docs/core-infrastructure.md`](docs/core-infrastructure.md) for the full
rationale.

- [x] `internal/job` -- typed job lifecycle state machine (pending →
      preparing → dumping/backing_up → verifying → completed/failed/
      cancelled/interrupted), SQLite-backed (pure-Go driver, WAL mode),
      with reconciliation of orphaned in-progress jobs to `interrupted`
      after an unclean restart. See
      [`docs/job-lifecycle.md`](docs/job-lifecycle.md).
- [x] `internal/scheduler` -- schedule/next-run calculation
      (hourly/daily/weekly, explicit IANA timezone, DST-correct),
      missed-run handling, and bounded exponential backoff with
      injectable jitter. Pure calculation, no daemon loop. See
      [`docs/scheduler.md`](docs/scheduler.md).
- [x] `internal/event` -- structured, append-only operational events
      (job/dump/backup/verification/restore lifecycle), a closed set of
      safe metadata fields (no free-form map), SQLite-backed sink plus
      no-op/in-memory sinks for tests. See
      [`docs/events.md`](docs/events.md).
- [x] `internal/backup.Engine` integration -- every `Run` call creates a
      job record and advances it through the typed lifecycle, with
      structured events emitted at each phase. Job/event tracking is
      optional (`WithJobStore`/`WithEventSink`) and degrades safely
      rather than failing a backup if unconfigured or if the store
      itself has a problem at runtime -- see `internal/backup`'s package
      doc comment. `servervault backup` wires this up against a new
      `state_dir` config field.

Deliberately out of scope for this milestone (see
`docs/core-infrastructure.md`): SSH (no real caller until the control
plane exists), a general storage abstraction (Restic already abstracts
storage backends directly).

## v0.4.0-alpha.1 — Safe restore

Status: complete on `feature/restore-v0.4.0-alpha.1`, pending review/merge.

- [x] `internal/restic` -- added `Restore`, `Stats`, `List` (the one
      deliberate, scoped addition beyond Phase A; `Init`/`Forget`/
      `Prune`/`Unlock` remain entirely absent)
- [x] `internal/postgres` -- added `RestoreToTemp`, `CreateDatabase`,
      `DatabaseExists`, `DropDatabase`, `PingDatabase`
- [x] `internal/restore` -- `Planner` (read-only, real repository
      metadata via `restic stats`/`restic ls`, never guessed) and
      `Executor` (dedicated restore lock, refuses to run alongside a
      backup, revalidates critical assumptions immediately before
      writing, every restore recorded in `internal/job` history and
      `internal/event`, cleanup on success/failure/cancellation). See
      [`docs/restore-flow.md`](docs/restore-flow.md).
- [x] `servervault snapshots`, `servervault restore --target
      files|temp-db [--path] [--database] [--dry-run] [--yes] [--output
      text|json]`
- [x] Integration tests: real snapshot created via a real
      `backup.Engine.Run`, then restored for real (files and temp-db,
      including cancellation and revalidation-triggered rejection) --
      see [`docs/testing.md`](docs/testing.md)

Retention (`internal/retention`, `servervault prune`) and
`servervault verify` move to their own later milestones -- see the
approved execution roadmap below.

## Beyond v0.4.0: the approved execution roadmap

The near-term milestones above (`v0.3.5` through `v0.4.0-alpha.1`) are
the first two of a larger, 19-milestone execution plan covering CLI
completion, local agent maturity, the control plane, the web platform,
and enterprise/scale features through `v2.0.0` -- each milestone
independently releasable and backward compatible with everything before
it. That plan (objectives, packages, APIs, tests, docs, CI, acceptance
criteria, rollback strategy, and release tag per milestone) was reviewed
and approved outside this repository; only the milestones actually
underway are reflected here, to avoid the roadmap drifting from real
code -- see this file's own "dates intentionally omitted" convention
above, applied the same way to the wider plan.

## v0.5.0 — Operability

- [ ] `servervault status`
- [ ] `internal/notify` (optional failure notifications)
- [ ] `internal/health` checks wired into `doctor` and `status`
- [ ] Parity with the shell implementation's default retention and
      safety behavior

## v1.0.0 — Go implementation replaces shell as the primary path

- [ ] Migration guide from the shell implementation
- [ ] `go-rewrite` merged into `main`
- [ ] Shell scripts kept for reference / deprecated with a clear
      timeline

Dates are intentionally omitted — milestones are scoped by completeness,
not calendar time. See open issues and pull requests for current
progress.

## Beyond v1.0.0: the platform roadmap

`v0.1.x` through `v1.0.0` above take ServerVault from the stable shell
implementation to a complete, standalone Go CLI. A separate, larger
architecture proposal extends the roadmap further — toward a central
control plane, secure server agents, and multi-server/multi-tenant
management — as ten additional phases. That proposal is the authoritative
source for this section; only the phase status is kept here so it doesn't
drift.

| Phase | Scope | Status |
| --- | --- | --- |
| 0 | Architecture and security design | Done — see [`docs/threat-model.md`](docs/threat-model.md) |
| 1 | Stable core Go CLI and config | Foundation complete (`v0.2.0-alpha` above); backup engine (`v0.3.0`–`v1.0.0` above) in progress |
| 2 | Local agent service | Not started |
| 3 | Single-server control plane | Not started |
| 4 | Secure enrollment and remote jobs | Not started |
| 5 | Multi-server management | Not started |
| 6 | Organizations, projects, RBAC | Not started |
| 7 | Web dashboard | Not started |
| 8 | Notifications, metrics, audit logs | Not started |
| 9 | Restore workflows and approvals | Not started |
| 10 | Production hardening and release candidate | Not started |

**Smallest safe MVP** for the platform is Phases 0–5: a fleet-manageable
agent platform for a single operator, before multi-tenancy, the web
dashboard, or restore approvals exist.

Docs for phases 2 and later (`docs/control-plane-architecture.md`,
`docs/agent-architecture.md`, `docs/api-design.md`, `docs/data-model.md`,
`docs/authentication.md`, `docs/authorization.md`, `docs/multi-tenancy.md`,
`docs/job-system.md`, `docs/observability.md`,
`docs/production-deployment.md`, `docs/upgrade-and-rollback.md`,
`docs/control-plane-backup.md`) are deliberately **not** written yet —
each gets written at the start of its own phase, against real code,
instead of speculatively now where it would only drift. Only
[`docs/threat-model.md`](docs/threat-model.md) was written ahead of time,
since a threat model is meant to guide the code that follows it rather
than describe code that already exists.
