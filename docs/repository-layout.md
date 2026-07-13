# Repository layout

```text
bin/                    Stable shell implementation (main)
  servervault-backup       Run a backup: dump, compress, restic backup, prune, check
  servervault-restore       Interactive, staging-first restore menu
  servervault-verify        Non-destructive repository verification (restic check)

cmd/servervault/        Go entry point (go-rewrite). Kept small: flag/command
                         wiring only, no business logic.

internal/                Go implementation packages (go-rewrite), not importable
                         outside this module.
  cli/                     Cobra command definitions; delegates to other packages
  config/                  Defaults, YAML loading, env overrides, validation
  doctor/                  Non-destructive environment/health checks
  logger/                  log/slog setup (text/JSON)
  execx/                   context-aware external command execution
  restic/                  Restic backend wrapper
  postgres/                pg_dump / pg_restore / verification
  backup/                  Backup orchestration
  restore/                 Staging-first restore orchestration
  retention/               Snapshot retention/prune policy
  lock/                    Concurrency guard (prevents overlapping backups)
  health/                  Health check primitives used by doctor/status
  notify/                  Optional failure notifications

configs/                 Example configuration, checked in with placeholders only.
  servervault.env.example   Shell implementation config (main)
  servervault.example.yaml  Go rewrite config (go-rewrite)
  logging.example.yaml      Go rewrite logging config
  excludes.txt               Default Restic exclude patterns

systemd/                Unit files for the shell implementation's scheduled jobs.

install.sh              Installs the shell implementation on a fresh host.

docs/                    This documentation.

.github/                 CI workflows, issue/PR templates, CODEOWNERS.
.vscode/                 Editor settings, tasks, and debug configurations.

testdata/                Fixtures for Go tests (populated as tests are added).
examples/                Backend-specific configuration examples (S3, B2,
                         Hetzner Storage Box, Docker, MariaDB, PostgreSQL).
```

## Two implementations, one repository

`main` and `go-rewrite` intentionally coexist in the same repository
during the rewrite:

- `main` — the shell implementation (`bin/`, `install.sh`, `systemd/`)
  is what gets installed in production today. It only receives bug
  fixes.
- `go-rewrite` — the Go CLI (`cmd/`, `internal/`) is being built up to
  reach parity before it replaces the shell implementation. See
  [`ROADMAP.md`](../ROADMAP.md) for milestone status.

Both branches share `docs/`, `.github/`, `configs/`, and other
supporting directories, so documentation and CI apply to whichever
implementation is checked out.
