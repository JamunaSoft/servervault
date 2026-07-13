# ServerVault Project Status

## Repository
- GitHub: `git@github.com:JamunaSoft/servervault.git`
- Public URL: `https://github.com/JamunaSoft/servervault`
- Stable branch: `main`
- Development branch: `go-rewrite`

## Current commits
- `main`: PostgreSQL verification fix commit `698ae03`
- `go-rewrite`: `49d36c3` — includes v0.3.5 (core infrastructure +
  `internal/backup` job/event integration), merged via PR #1. Tag
  `v0.3.0-alpha` (predates this merge) is published as a pre-release.
- `feature/restore-v0.4.0-alpha.1`: rebased onto the `go-rewrite` tip
  above; carries both v0.3.5 and v0.4.0-alpha.1's work in one linear
  history. Not yet merged into `go-rewrite`.

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
  `config validate`, `backup` (now with job/event tracking).
- Additionally working on `feature/restore-v0.4.0-alpha.1` (rebased onto
  `go-rewrite`, not yet merged): `snapshots`, `restore`.
- v0.2.0-alpha (CLI foundation), v0.3.0 Phase A (Restic+PostgreSQL
  backup engine), and v0.3.5 (core infrastructure + backup integration)
  are all complete and merged into `go-rewrite` — see `ROADMAP.md` for
  the full, current package-by-package checklist.

## Current milestone

```text
✅ v0.3.0-alpha tag pushed; published as a pre-release
✅ v0.3.5 (core infrastructure: internal/job, internal/scheduler,
   internal/event, internal/backup integration) merged into go-rewrite
   via PR #1
✅ main branch protection enabled
✅ v0.4.0-alpha.1 (safe restore) implementation complete on
   feature/restore-v0.4.0-alpha.1, rebased onto the current go-rewrite
   tip, builds and tests clean -- awaiting PR review and CI

Status: v0.4.0-alpha.1 awaiting PR review and CI

Current branch: go-rewrite has v0.3.5; feature/restore-v0.4.0-alpha.1
carries v0.4.0-alpha.1 on top of it, not yet merged -- see AI_MEMORY.md
for the sessions that produced both, including the rebase conflict
found and resolved after the fact (a prior partial conflict-resolution
commit left literal git conflict markers in internal/config/config.go
and internal/config/validate_test.go, breaking the build; fixed and
reverified).

Blocked by:
- feature/restore-v0.4.0-alpha.1 PR review and merge into go-rewrite
- First real CI run against this exact rebased commit -- in particular
  internal/restore's integration tests against real restic/PostgreSQL
  in postgres-integration, not yet observed
- 2-3 more consecutive green postgres-integration CI runs before it's
  promoted to a required branch-protection check
```

Do not build MySQL/retention/health/notifications in Go, and do not merge
`go-rewrite` into `main`, until the items above are closed and the
relevant milestone's design has been reviewed — see `ROADMAP.md` and
`AI_MEMORY.md`.
