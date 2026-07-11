# ServerVault Project Status

## Repository
- GitHub: `git@github.com:JamunaSoft/servervault.git`
- Public URL: `https://github.com/JamunaSoft/servervault`
- Stable branch: `main`
- Development branch: `go-rewrite`

## Current commits
- `main`: PostgreSQL verification fix commit `698ae03`
- `go-rewrite`: Cobra CLI and version command commit `cc618f3`

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
- Working command: `servervault version`
- Current files:
  - `cmd/servervault/main.go`
  - `internal/cli/version.go`

## Immediate milestone: v0.2.0-alpha
1. Root CLI package
2. Config package
3. YAML config
4. Environment overrides
5. Structured logging with `log/slog`
6. `servervault doctor`
7. `servervault config validate`
8. Build metadata with `-ldflags`
9. Makefile
10. Go CI
11. Unit tests

Do not build the production backup engine in Go before this foundation is stable.
