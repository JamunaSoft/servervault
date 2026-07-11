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

- [ ] `internal/restic` wrapper (context-aware, cancellable)
- [ ] `internal/postgres` dump/verify
- [ ] `internal/mysql` dump/verify (MySQL/MariaDB — scope addition beyond
      the original `CLAUDE.md` package list, needed for the platform's
      stated multi-database requirement)
- [ ] `internal/backup` orchestration
- [ ] `internal/lock` to prevent concurrent runs
- [ ] `servervault backup`

## v0.4.0 — Go restore and retention

- [ ] `internal/restore` (staging-first restore, temp-DB restore)
- [ ] `internal/retention` (`servervault prune`)
- [ ] `servervault snapshots`, `servervault restore`, `servervault verify`

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
