# CLAUDE.md — ServerVault Development Instructions

You are working on **ServerVault**, an open-source Linux server backup and disaster recovery toolkit.

Read first:
1. `PROJECT_STATUS.md`
2. `README.md`
3. Existing shell implementation in `bin/`
4. Current Go code in `cmd/` and `internal/`

## Branch policy
- `main` is stable production shell code.
- `go-rewrite` is active Go development.
- Do not merge incomplete Go work into `main`.
- Never force-push unless explicitly requested.

## Product goal
Provide safe, auditable, reproducible backup and disaster recovery.

Target CLI:

```bash
servervault version
servervault doctor
servervault config validate
servervault backup
servervault verify
servervault snapshots
servervault restore
servervault prune
servervault status
```

## Non-negotiable safety rules
1. Never delete a Restic repository automatically.
2. Never overwrite a live database by default.
3. Restore databases into a temporary DB by default.
4. Restore files into staging by default.
5. Never print or commit secrets.
6. Never assume a mount is remote without verification.
7. Refuse destructive cleanup if repository validation fails.
8. Prevent concurrent backups.
9. External commands must support context cancellation.
10. Never build shell commands from unsanitized input.

## Go architecture
Suggested packages:

```text
cmd/servervault/
internal/cli/
internal/config/
internal/doctor/
internal/logger/
internal/execx/
internal/restic/
internal/postgres/
internal/backup/
internal/restore/
internal/retention/
internal/lock/
internal/health/
internal/notify/
```

Rules:
- Keep `cmd/servervault` small.
- Cobra is only CLI wiring.
- Business logic must not depend on Cobra.
- Use `context.Context`.
- Wrap errors with operation context.
- Avoid globals.
- Prefer table-driven tests.
- Keep platform-specific code isolated.

## Config design
Layering:
1. Safe defaults
2. YAML
3. Environment
4. CLI flags

Precedence:
`flags > environment > YAML > defaults`

Validate:
- repository
- password file
- backup paths
- retention values
- PostgreSQL settings
- restore destinations
- backend syntax

## Doctor command
`servervault doctor` must be non-destructive and check:
- OS/architecture
- required commands
- config readability
- secret permissions
- Restic repository access
- PostgreSQL connectivity
- backup paths
- local disk space
- repository lock state
- SSH/SFTP non-interactive access
- systemd/timers
- timezone/schedule

Exit codes:
- `0` all required checks pass
- `1` one or more required checks fail
- `2` config or usage error

## Logging
Use `log/slog`.
- Human text by default
- JSON optional
- Never log secrets
- Include operation, duration, host, snapshot ID where available

## Required checks before commit

```bash
gofmt -w .
go test ./...
go vet ./...
go build ./cmd/servervault
```

If shell changes:

```bash
bash -n bin/*
shellcheck bin/* install.sh
```

## Commit style

```text
feat(cli): add doctor command
feat(config): load YAML with environment overrides
fix(postgres): verify compressed dumps safely
test(config): cover precedence and validation
docs: add SFTP example
```

## Current priority
Work on `go-rewrite` and complete:
1. root CLI package
2. build metadata
3. config loader
4. config validation
5. doctor
6. logging
7. tests
8. CI

Do not implement the Go backup engine until these are complete.
