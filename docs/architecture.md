# Architecture

ServerVault has two implementations living side by side during the Go
rewrite. See [`docs/repository-layout.md`](repository-layout.md) for
where each one lives on disk.

## Shell implementation (`main`)

A small set of independent, `set -Eeuo pipefail` bash scripts, each
doing one job:

- `servervault-backup` — dumps PostgreSQL, compresses with Zstandard,
  runs `restic backup`, prunes with `restic forget --prune`, then
  verifies with `restic check`. Guarded by `flock` to prevent
  concurrent runs.
- `servervault-restore` — interactive menu that always restores into a
  new, timestamped directory under `RESTORE_ROOT`; never overwrites
  live paths.
- `servervault-verify` — runs `restic check --read-data` on a schedule,
  independent of backup runs.

Configuration is a single sourced env file
(`/etc/servervault/servervault.env`); see
[`docs/configuration.md`](configuration.md). Scheduling is systemd
timers (`systemd/`).

## Go rewrite (`go-rewrite`)

Structured as a small `cmd/servervault` entry point plus focused
`internal/` packages, per the design rules in
[`CLAUDE.md`](../CLAUDE.md):

```text
cmd/servervault  --(wires)-->  internal/cli
                                    |
                                    v
                 internal/{config,doctor,logger}   (foundation, current milestone)
                                    |
                                    v
        internal/{restic,postgres,backup,restore,retention,lock,health,notify}
                                    (backup engine, later milestone)
```

Design rules that shape the package boundaries:

- `cmd/servervault` only builds the Cobra command tree and calls
  `internal/cli`. It contains no business logic.
- Cobra is confined to `internal/cli`. Every other package is a plain
  Go library with no Cobra dependency, so it can be tested and reused
  without a CLI context.
- External processes (`restic`, `pg_dump`, `pg_restore`, `ssh`) are
  invoked through `internal/execx`, which accepts a `context.Context`
  so every external command is cancellable.
- Errors are wrapped with operation context (`fmt.Errorf("...: %w",
  err)`) so failures are traceable without needing to log secrets.
- No package-level mutable global state; dependencies are passed in
  explicitly (config, logger, executor).

## Why the split

Rewriting a production backup tool in place, on the branch that
production depends on, is how you lose backups. `go-rewrite` is where
the new implementation is built and tested until it has genuine parity
with the shell implementation's safety guarantees — see
[`docs/security-model.md`](security-model.md) — at which point it
replaces `main`.
