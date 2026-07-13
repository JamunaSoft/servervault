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

The config package (`internal/config`) loads configuration in layers, each
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

List-valued variables are delimited differently depending on whether their
values can contain spaces:

- `SERVERVAULT_BACKUP_PATHS` (filesystem paths) splits on **commas only**,
  so a path containing a space (e.g. `/var/www/My Site`) is never split in
  two. Whitespace around each comma-separated entry is trimmed.
- `SERVERVAULT_RESTIC_TAGS` (Restic tags) splits on commas **or**
  whitespace, since tags are simple identifiers.

Copy [`configs/servervault.example.yaml`](../configs/servervault.example.yaml)
and [`configs/logging.example.yaml`](../configs/logging.example.yaml),
strip the `.example` suffix, and edit — again, never commit the edited
copies.

### PostgreSQL authentication

Leave `postgres.host` empty (the default). ServerVault then connects via
the local Unix socket and runs `pg_dump`/`psql` as `postgres.user` through
non-interactive `sudo`, relying on PostgreSQL peer authentication — the
same model the shell implementation uses, with no password anywhere.
Setting `postgres.host` switches to a TCP connection, which needs a
different, password-based auth setup this tool does not manage.

### Locking

`backup.lock_file` (default `/run/lock/servervault-backup.lock`) prevents
two backups from running concurrently — see
[`docs/security-model.md`](security-model.md). It deliberately defaults to
the same path the shell implementation's `servervault-backup` uses, so a
shell-driven and a Go-driven backup mutually exclude each other during a
migration period where both might be scheduled.

### Validation

`servervault config validate` checks, without making any filesystem
changes (structural/shape validation only):

- the repository URL parses for a supported backend
- the password file path is set and absolute
- backup paths are set and absolute
- retention values are non-negative and not all zero
- PostgreSQL connection settings are well-formed
- restore destinations don't overlap a live backup path, and the temp
  database prefix doesn't equal the live database name (see
  [`docs/security-model.md`](security-model.md))
- backend-specific syntax (e.g. `sftp:`, `s3:`, `b2:` prefixes)

`servervault doctor` covers the filesystem/environment-reality checks
`config validate` deliberately doesn't (does the password file actually
exist with safe permissions, are backup paths actually present, is the
Restic repository actually reachable, is PostgreSQL actually reachable).

Exit codes follow the `doctor` convention — see
[`docs/testing.md`](testing.md) and the project's `doctor` design in
[`CLAUDE.md`](../CLAUDE.md).

## What never goes in configuration files checked into git

Repository passwords, SSH private keys, database passwords, and any
production connection string. `.gitignore` already excludes the
common patterns (`*.env`, `servervault.env`, `*-password`, `*.key`,
`*.pem`); the `.example` files in `configs/` must only ever contain
placeholders.
