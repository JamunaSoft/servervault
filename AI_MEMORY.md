# AI Memory Log

This file is a running handoff log for AI coding agents (Claude Code,
Copilot, Codex, Kilo Code, etc.) working on ServerVault across sessions.
It is a supplement to — not a replacement for — `CLAUDE.md`, `AGENTS.md`,
and `PROJECT_STATUS.md`, which hold the durable instructions and current
status. Use this file for things that don't belong in those: decisions
made, dead ends explored, and context that would otherwise be lost
between sessions.

## How to use this file

- **Read first.** Before starting work, skim the most recent entries.
- **Append, don't rewrite.** Add a new dated entry per session; do not
  delete or rewrite prior entries unless they are actively wrong.
- **No secrets.** Never record hostnames, credentials, tokens, private
  IPs, or production paths here — this file is committed to git. Use
  placeholders (`HOST`, `USER`, `example.com`) the same way the rest of
  the repository does.
- **Be concrete.** Prefer "chose X over Y because Z" to vague status
  updates — the goal is to save the next agent from re-deriving a
  decision that was already made.

## Entry format

```markdown
## YYYY-MM-DD — short title

- Branch:
- What changed:
- Decisions / rationale:
- Open questions / follow-ups:
```

## Log

## 2026-07-11 — Repository scaffolding

- Branch: `go-rewrite`
- What changed: filled in previously empty placeholder files across
  `.github/`, `.vscode/`, `configs/`, and `docs/` — CI workflows,
  issue/PR templates, editor config, example configs, and
  documentation. No changes to Go source or shell scripts beyond fixing
  a stale `config/` path reference in `install.sh` after the
  `config/ -> configs/` rename.
- Decisions / rationale: kept scope to documentation/tooling scaffolding
  per `CLAUDE.md`'s "current priority" — the Go backup engine is
  explicitly deferred until the CLI/config/doctor/logging foundation is
  complete.
- Open questions / follow-ups: `internal/config`, `internal/doctor`,
  `internal/logger`, and friends are still empty packages; that is the
  next milestone (see `ROADMAP.md` v0.2.0-alpha).

## 2026-07-11 — Platform architecture proposal + v0.2.0-alpha foundation

- Branch: `go-rewrite`
- What changed: (1) delivered a full architecture proposal for evolving
  ServerVault into a multi-server control-plane/agent platform (Phases
  0–10) as an artifact, approved for Phases 0–1 only; (2) implemented
  `v0.2.0-alpha`'s foundation for real: `internal/version`,
  `internal/execx`, `internal/config` (types, YAML+env loading,
  filesystem-free validation), `internal/logger`, `internal/doctor`
  (7 real checks + 5 explicitly `SKIP`ped checks pending the backup
  engine), and CLI wiring (`servervault doctor`, `servervault config
  validate`, refactored `servervault version`). Every package ships with
  table-driven tests. Fixed the `-X main.Version=...` ldflags in
  `Makefile`/`release.yml` to target `internal/version` now that it
  exists (resolves the TODO left in the prior session). Added
  `docs/threat-model.md` and a platform-roadmap summary table in
  `ROADMAP.md`.
- Decisions / rationale: scoped "Phase 1 foundations" to exactly
  `docs/architecture.md`'s existing "foundation, current milestone" tier
  (`config`, `doctor`, `logger`, plus `version`/`execx` as supporting
  plumbing) and explicitly excluded the backup engine
  (`restic/postgres/mysql/backup/restore/retention/lock/health/notify`)
  — that tier is already labeled "later milestone" in the same diagram,
  and CLAUDE.md says not to start it yet. `doctor` checks that need the
  backup engine (repository access, PostgreSQL connectivity, lock state,
  SSH reachability, systemd/timers) report `StatusSkip` with a reason
  rather than being faked or silently omitted — `Report.Failed()` only
  reacts to `StatusFail`, so skips never block a clean exit code.
  `config.Validate` is deliberately filesystem-free (structural checks
  only); `doctor` owns the I/O-requiring checks (file existence,
  permissions) — this split maps directly to the two separate check
  lists in `CLAUDE.md` ("Config design: Validate" vs. "Doctor command").
  Of the 13 platform docs proposed in the architecture artifact, only
  `docs/threat-model.md` was written now; the rest are deferred to their
  own phases to avoid speculative documentation that drifts from
  not-yet-built code.
- Open questions / follow-ups: `internal/{restic,postgres,mysql,backup,
  restore,retention,lock,health,notify}` are the next milestone
  (`ROADMAP.md` v0.3.0+). The full platform proposal (Phases 2–10) is
  still awaiting a build decision from the user beyond Phases 0–1.

## 2026-07-12 — v0.3.0 Phase A: Restic + PostgreSQL backup engine

- Branch: `go-rewrite`
- What changed: implemented `internal/lock`, `internal/restic`,
  `internal/postgres`, `internal/backup`, doctor integration for all
  three, and `servervault backup`. Full design (interfaces, state flow,
  error taxonomy, failure/cleanup matrix, security risks) was reviewed
  and approved before implementation; see that turn's design doc for the
  complete rationale — this entry only records what changed since the
  design and why.
- Decisions / rationale:
  - **No `internal/repository` abstraction.** Considered and declined:
    `internal/backup` already defines consumer-side `Dumper`/`Backer`
    interfaces that `*restic.Repository`/`*postgres.Client` satisfy
    structurally — a future Kopia/Borg implementation slots in there
    without touching `internal/backup`. A producer-side interface with
    one implementation risked being the wrong abstraction.
  - **Lock path unchanged from the shell implementation**
    (`/run/lock/servervault-backup.lock`, now `config.Backup.LockFile`).
    Deliberate: during a shell→Go migration where both might be
    scheduled, sharing the lock path is what makes them mutually
    exclusive. A per-operation-type directory scheme was considered and
    deferred to when `verify`/`restore` commands actually exist.
  - **Restic exit code 3** ("some source files unreadable") is a
    success with `Result.Warnings` populated, not a Go error — common
    and expected (permission-denied on an ephemeral file), and
    restic still produces a usable snapshot. Every other non-zero exit
    is a hard failure.
  - **Real bug caught during design→implementation**: Phase 1's
    `PostgresConfig` defaulted `Host` to `"127.0.0.1"`, but the shell
    implementation deliberately omits `-h`/`-p` so `psql`/`pg_dump`
    connect via the local Unix socket, which is required for peer
    authentication (`sudo -u <user>`, no password) to apply. Shipping
    that default would have silently broken auth for anyone relying on
    defaults — exactly the real production setup this project models.
    Fixed: `Host` now defaults to `""`; `internal/postgres` only adds
    `-h`/`-p` when `Host` is explicitly set.
  - **`internal/postgres` wraps `pg_dump`/`pg_restore`/`psql` CLIs**, not
    a Go SQL driver — preserves the shell's exact peer-auth model with no
    new dependency. Same reasoning for `zstd` (shelled out, not a native
    Go compression library).
  - `execx.Runner`/`RunOptions` (streaming, `Env`, `Cancel`=SIGTERM then
    `WaitDelay`=5s before SIGKILL) is new, generalizing the old
    `execx.Run`, which now delegates to it — no breaking change to
    Phase 1 callers/tests.
  - Declined for now (YAGNI, no consumer exists yet): structured event
    types and a `MetricsRecorder` interface. `log/slog`'s structured
    fields already cover current needs; both belong in the platform
    proposal's Phase 8, against a real backend to validate the shape.
  - Accepted: `doctor --json` (small, isolated, real value); two Mermaid
    diagrams added to `docs/backup-flow.md` instead of a full ADR
    process for this phase.
- Open questions / follow-ups: `internal/mysql` not started (flagged,
  not yet confirmed for priority). `internal/{restore,retention,health,
  notify}` are the next milestones (`ROADMAP.md` v0.4.0+). No
  integration tests against a real Restic binary were added (not
  installed in the dev/CI sandbox) — argv/logic correctness is covered
  via fakes; consider a `//go:build integration` suite skip-guarded on
  `exec.LookPath("restic")` if/when a CI environment has it.

## 2026-07-12 — v0.3.0 Phase A integration test milestone

- Branch: `go-rewrite`
- What changed: added a real-binary integration test suite for Phase A
  (`internal/{restic,postgres,backup}/integration_test.go`,
  `internal/backup/concurrency_test.go`), a shared
  `internal/testsupport` helper package, deterministic exit-code-11
  (`ExitLockFailed`) unit tests, an opt-in `resticlock`-tagged real
  lock-conflict probe, `make test-integration`/`test-resticlock`
  targets, a `docs/testing.md` rewrite covering all of it, and two new
  CI workflows (`integration.yml`: restic required + postgres
  non-blocking; `restic-lock-probe.yml`: manual/scheduled only). Design
  was reviewed and approved (with adjustments) before implementation;
  this entry records what changed and the reasoning, not the full
  design (see that turn for the complete rationale).
- Decisions / rationale:
  - **Tried to install a real `restic` binary into scratchpad to verify
    its lock-retry behavior empirically before designing the probe test
    — blocked by the auto-mode classifier as an unauthorized
    download+execute.** Did not retry or work around it; designed the
    probe as best-effort/skip-on-uncertainty instead, and documented the
    uncertainty rather than guessing silently. This governed the final
    adjustment list (deterministic classification as a normal unit test;
    the real probe made opt-in and non-required).
  - **`internal/testsupport` (new shared package, `integration`-tagged)**:
    initially planned to duplicate "spin up a temp restic repo" / "spin
    up a temp postgres db" logic per package (three call sites), but the
    duplication included a safety-critical guard (never drop a database
    without the `servervault_test_` prefix) — three independent copies
    of a safety guard is a real risk of drift, not just style. One
    shared package with one guard implementation was the right call
    here, reversing the "no shared testutil" instinct from the original
    design sketch once the actual amount of safety-relevant logic became
    clear.
  - **`lockprobe_test.go` tagged `integration && resticlock`** (not just
    `resticlock` alone) specifically so it can reuse
    `testsupport.NewResticRepository` without duplicating it — requires
    the CI probe job to build with both tags.
  - **Cleanup-matrix scope decision**: item 6 asked for a "dump
    verification failure" case in the end-to-end (`internal/backup`)
    integration suite. Constructing that precisely at the `Engine.Run`
    level with a *real* corrupted dump would require racing to corrupt
    the file between Dump and VerifyDump — not reliably constructible
    without new production hooks. Split instead: real corrupted-dump
    detection is tested directly against `postgres.Client.VerifyDump`
    (`TestIntegration_VerifyDump_CorruptedFile`), and the
    orchestration guarantee ("verify failure ⇒ Restic never called") is
    already proven with fakes (`TestEngine_Run_VerifyFailureNeverCallsRestic`,
    Phase A). Go's control flow through that branch doesn't differ
    between a real and a fake `VerifyDump` error, so re-proving the
    orchestration with a real binary wouldn't add coverage, only
    flakiness risk.
  - **`TestIntegration_Run_PostgresConnectivityFailure_CleansUp` doesn't
    require `restic`** (uses a structurally-valid but never-invoked
    `ResticConfig`), specifically so it still runs in the
    postgres-integration CI job, which doesn't install restic at all —
    caught this by actually running the suite locally (restic absent,
    postgres client tools present but no sudo) and noticing the test
    skipped for the wrong reason.
  - **PostgreSQL peer-auth CI job creates a dedicated `servervault_test`
    OS+DB role** (`createuser --createdb`, not superuser) rather than
    running as `postgres` — least privilege, and a truer exercise of the
    identity-mapping peer-auth path than reusing the admin role would be.
  - **CI verification steps** (`restic version`, `sudo -u
    servervault_test psql -Atc 'SELECT 1'`) added right after each
    install step specifically so a broken CI setup fails the job instead
    of surfacing as misleadingly-passing skipped tests — the per-test
    skip logic itself is unchanged from local-dev behavior.
- Open questions / follow-ups: `postgres-integration` CI job is
  `continue-on-error: true` per the approved plan — worth revisiting
  (promote to required) once it's proven stable over a few runs.
  `restic-integration` and the lock probe have not been run against a
  real GitHub Actions runner yet (only locally, where `restic` is absent
  and the suite correctly skips) — first real CI run is the actual
  verification of the YAML/install steps themselves.

## 2026-07-13 — v0.3.5 Core infrastructure (autonomous session)

- Branch: `feature/core-infrastructure-v0.3.5` (off `go-rewrite` at
  `23ab3c9`; not merged)
- What changed: implemented `internal/job` (typed lifecycle state
  machine, SQLite-backed via a pure-Go driver, optimistic-concurrency
  transitions, crash reconciliation), `internal/scheduler`
  (hourly/daily/weekly next-run, explicit timezone/DST handling,
  missed-run policy, bounded exponential backoff with injectable
  jitter), and `internal/event` (structured append-only events, closed
  metadata schema, SQLite + no-op/in-memory sinks). Added
  `modernc.org/sqlite` as a new dependency. Wrote
  `docs/{core-infrastructure,job-lifecycle,scheduler,events}.md` and
  extended `docs/testing.md`. This was executed under an explicit
  autonomous-session brief (branch-per-milestone, no merges/releases/
  force-pushes, extensive hard safety rules) — see that session's full
  final report for the complete accounting of tests run, files changed,
  and stop-condition checks.
- Decisions / rationale:
  - **SQLite driver: `modernc.org/sqlite`, pinned to `v1.34.4`**, not the
    latest release — the latest (`v1.53.0`) requires Go ≥ 1.25, but this
    module targets Go 1.22.2 per `go.mod`/CI. `v1.34.4` is the newest
    version whose own `go.mod` declares `go 1.21`, confirmed by querying
    `proxy.golang.org` directly rather than guessing. Pure Go (no cgo),
    preserving the static-binary build.
  - **`MaxOpenConns(1)` on every `*sql.DB`**, not a separate in-process
    mutex plus multiple connections. For a local, single-process job
    store this is simpler and more predictable than tuning
    `busy_timeout` against real multi-connection contention, and it's
    what makes the optimistic-concurrency `Advance` compare-and-swap
    (an internal `row_version` column) safe under `go test -race`
    without additional locking. Revisit if a future milestone needs
    genuine multi-connection throughput against one file.
  - **No down migrations for `internal/job`/`internal/event` schemas** —
    unlike the control-plane migrations in the wider roadmap (which do
    require tested down migrations or a documented forward-fix plan),
    this is purely local, disposable operational history: losing it
    means losing history, never live backup data, so a rollback path
    wasn't judged worth the added complexity.
  - **Hand-rolled `Schedule` type (frequency + time-of-day + weekday +
    location), not a cron-expression parser or a third-party cron
    library.** The roadmap only ever needs "daily/weekly/hourly at a
    given wall-clock time"; a full cron grammar would be speculative
    generality with no current consumer, and a third-party dependency
    was explicitly disfavored by this session's brief unless strongly
    justified. DST correctness is delegated to Go's own `time.Date`
    normalization rather than reimplemented.
  - **`job.Metadata`/`event.Metadata` are closed, typed structs, not
    `map[string]string`** — no generic setter exists anywhere in either
    package's public API, so a secret cannot be attached to persisted
    history structurally, not just by convention. Both carry a
    reflection-based regression test denylisting secret-shaped field
    names, so a future careless addition fails the build.
  - **Deliberately declined, despite being listed as a v0.3.5 acceptance
    criterion in the approved roadmap: wiring `internal/job`/
    `internal/event` into the already-shipped `internal/backup.Engine`.**
    The autonomous session's own hard rules ("do not rewrite completed
    packages unless a failing test or verified defect requires a small,
    targeted fix," "existing backup tests remain green") were weighted
    above that one acceptance line — retrofitting a stable, tested
    package inside the same session that built the thing being
    retrofitted was judged higher-risk than deferring it. `internal/
    restore` (v0.4.0-alpha.1, built next in the same session) is the
    first real production consumer instead, proving the design against
    real usage before `internal/backup` is touched in a future, narrowly
    -scoped change. Documented explicitly in `docs/core-infrastructure.md`,
    `ROADMAP.md`, and the session's final report rather than silently
    dropped.
  - **Real crash-consistency test, not a simulated one**:
    `TestStore_ReconcileAfterUncleanRestart` spawns the test binary
    itself as a subprocess (`-test.run=TestHelperProcess_CrashMidJob`),
    which creates a job, advances it, and sends itself `SIGKILL` with no
    graceful shutdown — the standard Go subprocess-test pattern (also
    used by `os/exec`'s own tests), chosen over asserting SQLite's WAL
    durability by claim alone.
- Open questions / follow-ups: the `feature/core-infrastructure-v0.3.5`
  branch has not been reviewed or merged into `go-rewrite` — that's a
  human decision, not automated by this session. `internal/backup`
  retrofit onto `internal/job`/`internal/event` remains unscheduled (see
  above). Where the shared SQLite state file lives on disk in a real
  deployment (an `agent.state_dir`-style config field) is left for
  v0.9.0's Local Agent milestone, since only a long-running daemon
  actually owns a persistent state directory today.
