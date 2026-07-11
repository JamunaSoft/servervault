# Configuration

ServerVault has two configuration formats today, one per implementation.

## Shell implementation (`main`): env file

A single file, sourced by every `bin/servervault-*` script — default
path `/etc/servervault/servervault.env`, overridable with
`SERVERVAULT_CONFIG`. Copy
[`configs/servervault.env.example`](../configs/servervault.env.example)
and edit it in place; never commit the edited copy.

Key variables:

| Variable | Purpose |
| --- | --- |
| `RESTIC_REPOSITORY` | Restic backend URL |
| `RESTIC_PASSWORD_FILE` | Path to the repository password file (mode `0600`) |
| `RESTIC_SFTP_COMMAND` | Optional SSH command override for the sftp backend |
| `POSTGRES_DATABASE` / `POSTGRES_USER` | Database to dump |
| `BACKUP_ROOT` / `RESTORE_ROOT` | Local working directories |
| `EXCLUDES_FILE` | Restic `--exclude-file` |
| `BACKUP_PATHS` | Space-separated list of paths to back up |
| `KEEP_DAILY` / `KEEP_WEEKLY` / `KEEP_MONTHLY` | Retention counts |
| `ZSTD_LEVEL` | Compression level (1-19) |
| `SERVER_TAG` | Host/snapshot tag, defaults to `hostname -s` |

The file is `source`d directly, so it must be valid bash and must never
be writable by anyone but root (`install.sh` installs it `0600`).

## Go rewrite (`go-rewrite`): layered YAML config

The config package (`internal/config`, in progress — see
[`ROADMAP.md`](../ROADMAP.md)) loads configuration in layers, each
overriding the last:

```text
1. Safe defaults
2. YAML file        (configs/servervault.example.yaml is the template)
3. Environment       (SERVERVAULT_* variables)
4. CLI flags
```

Precedence: **flags > environment > YAML > defaults.**

Environment variables mirror the YAML structure with the
`SERVERVAULT_` prefix and underscores for nesting, e.g.
`restic.repository` becomes `SERVERVAULT_RESTIC_REPOSITORY`.

Copy [`configs/servervault.example.yaml`](../configs/servervault.example.yaml)
and [`configs/logging.example.yaml`](../configs/logging.example.yaml),
strip the `.example` suffix, and edit — again, never commit the edited
copies.

### Validation

`servervault config validate` (planned) checks, without making any
changes:

- the repository URL parses for a supported backend
- the password file exists and is not world/group readable
- backup paths exist and are readable
- retention values are non-negative and sane relative to each other
- PostgreSQL connection settings are well-formed
- restore destinations are writable and not inside a live path
- backend-specific syntax (e.g. `sftp:`, `s3:`, `b2:` prefixes)

Exit codes follow the `doctor` convention — see
[`docs/testing.md`](testing.md) and the project's `doctor` design in
[`CLAUDE.md`](../CLAUDE.md).

## What never goes in configuration files checked into git

Repository passwords, SSH private keys, database passwords, and any
production connection string. `.gitignore` already excludes the
common patterns (`*.env`, `servervault.env`, `*-password`, `*.key`,
`*.pem`); the `.example` files in `configs/` must only ever contain
placeholders.
