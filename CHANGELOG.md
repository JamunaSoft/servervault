# Changelog

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
