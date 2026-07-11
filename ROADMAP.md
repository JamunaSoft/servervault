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

Status: in progress.

- [x] Cobra root CLI and `servervault version`
- [ ] Config package: defaults, YAML, environment overrides, flags
- [ ] `servervault config validate`
- [ ] Structured logging with `log/slog`
- [ ] `servervault doctor` (non-destructive environment checks)
- [ ] Build metadata via `-ldflags`
- [ ] Unit test coverage for config, doctor, and logging packages
- [ ] Go CI (`gofmt`, `go vet`, `go test`, `go build`)

The Go backup/restore engine is intentionally **not** started until this
foundation is stable — see the "Current priority" section of
[`CLAUDE.md`](CLAUDE.md).

## v0.3.0 — Go backup engine

- [ ] `internal/restic` wrapper (context-aware, cancellable)
- [ ] `internal/postgres` dump/verify
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
