# ServerVault Project Status

## Repository
- GitHub: `git@github.com:JamunaSoft/servervault.git`
- Public URL: `https://github.com/JamunaSoft/servervault`
- Stable branch: `main`
- Development branch: `go-rewrite`

## Current commits
- `main`: `149ab15` — stable shell implementation plus the v0.2.0-alpha
  Go foundation only. A PR (#2) briefly squash-merged the entire Go
  rewrite (including the not-yet-reviewed restore engine) into `main`
  by mistake (wrong base branch); PR #4 reverted it back out. Tag
  `v0.3.0-alpha` is published as a pre-release.
- `go-rewrite`: `2b7e276` — v0.2.0-alpha through v0.4.0-alpha.1 (safe
  restore, merged via PR #5) plus the platform architecture design
  documents (PR #6). This is the branch the mistaken `main` merge was
  ultimately, correctly reconciled into.
- `feature/retention-v0.5.0`: branched off the `go-rewrite` tip above;
  carries `internal/retention` (v0.5.0's retention engine, `servervault
  prune`) and `internal/notify` (v0.5.0's failure notifications,
  wrapping the existing event store rather than changing
  backup/restore/retention). Not yet merged into `go-rewrite`; no PR
  opened yet (no `gh` CLI or GitHub token available in the environment
  this was developed in -- see
  `https://github.com/JamunaSoft/servervault/pull/new/feature/retention-v0.5.0`).

## Production deployment already tested
- Host: `srv.eea.bd`
- OS: Ubuntu 24.04
- Application root: `/var/www/eeaid`
- Frontend: Next.js with PM2
- Backend: Laravel/PHP
- Database: PostgreSQL 15
- Database name: `eeaid_production`
- Application DB user: `eeaid_user`

Backup destination:
- Hetzner Storage Box user: `u624358`
- SSH alias: `hetzner-storage`
- SSH private key: `/root/.ssh/hetzner_backup`
- Restic repository: `sftp:hetzner-storage:eea.bd/restic-repository`
- Restic password file: `/root/.config/restic/servervault-password`

A production snapshot was created successfully and `restic check` returned `no errors were found`.

Example snapshot:
- ID: `a730d4a2`
- Host: `srv-eea-bd`
- Tags: `servervault,srv-eea-bd`

## Stable shell implementation
Features:
- PostgreSQL dump
- Zstandard compression
- Restic backup
- Website and server config backup
- Retention
- Verification
- Restore tooling
- systemd services and timers

Production paths:
- `/usr/local/sbin/servervault-backup`
- `/etc/servervault/servervault.env`
- `/etc/servervault/excludes.txt`

## Bug fixed
Broken code:

```bash
zstd -dc "$DB_DUMP" | pg_restore --list - >/dev/null
```

Fixed code:

```bash
TMP_DUMP="${DB_DUMP%.zst}"
zstd -dc "$DB_DUMP" > "$TMP_DUMP"
pg_restore --list "$TMP_DUMP" >/dev/null
rm -f "$TMP_DUMP"
```

## Go rewrite status
- Go: 1.22.2
- Module: `github.com/JamunaSoft/servervault`
- Cobra: `v1.8.1`
- Working commands on `go-rewrite`: `servervault version`, `doctor`,
  `config validate`, `backup`, `snapshots`, `restore` (all with
  job/event tracking where applicable).
- Additionally working on `feature/retention-v0.5.0` (branched off
  `go-rewrite`, not yet merged): `prune`, plus notifications (no new
  command -- wired into `backup`/`restore`/`prune` via `notify.enabled`
  in config).
- v0.2.0-alpha (CLI foundation), v0.3.0 Phase A (Restic+PostgreSQL
  backup engine), v0.3.5 (core infrastructure + backup integration),
  and v0.4.0-alpha.1 (safe restore) are all complete and merged into
  `go-rewrite` — see `ROADMAP.md` for the full, current
  package-by-package checklist.

## Current milestone

```text
✅ v0.3.0-alpha tag pushed; published as a pre-release
✅ v0.3.5 (core infrastructure) merged into go-rewrite via PR #1
✅ main branch protection enabled
✅ v0.4.0-alpha.1 (safe restore) merged into go-rewrite via PR #5,
   after a detour: PR #2 briefly merged it into main by mistake
   (wrong base branch), corrected by PR #4 (revert on main) and PR #5
   (proper merge into go-rewrite) -- see AI_MEMORY.md
✅ Platform architecture design documents (control-plane-architecture,
   agent-architecture, api-design, data-model, extensibility) merged
   into go-rewrite via PR #6
🚧 v0.5.0 retention (internal/retention, servervault prune) and
   notifications (internal/notify) implemented on
   feature/retention-v0.5.0, builds and tests clean locally -- awaiting
   PR review and CI. v0.5.0's remaining scope (status, health) is not
   started.

Status: v0.5.0 retention + notify awaiting PR review and CI; status/
health not started.

Blocked by:
- feature/retention-v0.5.0 PR review and merge into go-rewrite -- no PR
  opened yet (no gh CLI/token available in this environment; direct
  link: github.com/JamunaSoft/servervault/pull/new/feature/retention-v0.5.0)
- First real CI run against this exact commit -- in particular
  internal/retention's integration tests against real restic in
  restic-integration, not yet observed (no restic binary available in
  the environment this branch was developed in)
```

Do not build MySQL/health/notifications in Go, and do not merge
`go-rewrite` into `main`, until the items above are closed and the
relevant milestone's design has been reviewed — see `ROADMAP.md` and
`AI_MEMORY.md`.
