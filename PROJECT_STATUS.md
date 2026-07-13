# ServerVault Project Status

## Repository
- GitHub: `git@github.com:JamunaSoft/servervault.git`
- Public URL: `https://github.com/JamunaSoft/servervault`
- Stable branch: `main`
- Development branch: `go-rewrite`

## Current commits
- `main`: PostgreSQL verification fix commit `698ae03`
- `go-rewrite`: `d348f60` (tagged `v0.3.0-alpha`)

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
- Working commands: `servervault version`, `doctor`, `config validate`, `backup`
- v0.2.0-alpha (CLI foundation) and v0.3.0 Phase A (Restic+PostgreSQL
  backup engine) are both complete — see `ROADMAP.md` for the full,
  current package-by-package checklist.

## Current milestone

```text
✅ v0.3.0-alpha tag pushed; draft GitHub release built
✅ v0.3.5 (core infrastructure: internal/job, internal/scheduler,
   internal/event, plus internal/backup.Engine integration) complete
   on feature/core-infrastructure-v0.3.5, not yet merged into
   go-rewrite -- every acceptance criterion honestly met, including
   the internal/backup retrofit originally deferred
✅ v0.4.0-alpha.1 (safe restore) complete on
   feature/restore-v0.4.0-alpha.1, stacked on
   feature/core-infrastructure-v0.3.5 pending its merge, not yet
   merged into go-rewrite

Status: both feature branches ready for review; go-rewrite unchanged

Current branch: go-rewrite (work happened on
feature/core-infrastructure-v0.3.5 and feature/restore-v0.4.0-alpha.1 --
one branch per milestone, not merged automatically; see AI_MEMORY.md for
the autonomous sessions that produced them)

Blocked by:
- Draft release review + publish as pre-release
- Branch protection rules on main
- 2-3 more consecutive green postgres-integration CI runs
- Feature branch review + merge into go-rewrite: core-infrastructure
  first (it's the base of the stack), then rebase and merge restore
  (not opened automatically -- see AI_MEMORY.md)
```

Do not build MySQL/retention/health/notifications in Go, and do not merge
`go-rewrite` into `main`, until the items above are closed and the
relevant milestone's design has been reviewed — see `ROADMAP.md` and
`AI_MEMORY.md`.
