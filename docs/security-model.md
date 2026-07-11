# Security model

This is the threat model and design rationale behind the non-negotiable
safety rules in [`CLAUDE.md`](../CLAUDE.md) and [`AGENTS.md`](../AGENTS.md).
For how to report a vulnerability, see [`SECURITY.md`](../SECURITY.md).

## Threats considered

- **Accidental data loss by the operator** — the dominant risk for a
  backup tool. Most of this model is about making the *safe* action
  the default one, so a rushed or scripted invocation can't destroy
  data.
- **Credential exposure** — repository passwords, SSH keys, and
  database passwords leaking into logs, git history, or process
  listings.
- **Command injection** — untrusted input (paths, hostnames, database
  names sourced from config) being interpreted as shell syntax.
- **Concurrent execution** — two backup runs racing on the same
  repository or lock file.
- **Silent corruption** — a backup or dump that "succeeds" but isn't
  actually restorable.

## Design responses

| Rule | Threat addressed |
| --- | --- |
| Never delete a Restic repository automatically | Accidental data loss |
| Never overwrite a live database by default | Accidental data loss |
| Restore databases into a temporary DB by default | Accidental data loss |
| Restore files into staging by default | Accidental data loss |
| Never print or commit secrets | Credential exposure |
| Never assume a mount is remote without verification | Accidental data loss (backing up an unmounted/empty directory as if it were the real remote mount) |
| Refuse destructive cleanup if repository validation fails | Silent corruption compounding into data loss |
| Prevent concurrent backups | Concurrent execution races |
| External commands support context cancellation | Runaway/stuck operations holding locks or resources |
| Never build shell commands from unsanitized input | Command injection |

## How the shell implementation applies this today

- `servervault-backup` takes an exclusive `flock` before doing
  anything, so a second invocation exits immediately instead of
  racing.
- The PostgreSQL dump is verified (`zstd -t`, then `pg_restore --list`)
  *before* it's trusted, catching a corrupt dump before it's the only
  copy.
- `restic forget --prune` only removes snapshots outside the
  configured retention window, and only runs after a successful backup
  — a failed backup run never triggers pruning.
- `servervault-restore` only ever writes into new, timestamped
  directories under `RESTORE_ROOT`, or a template-named restore
  database — it has no code path that writes to a live path.
- Config is a `source`d file with `0600` permissions, installed by
  `install.sh`; `.gitignore` excludes `*.env`, `*-password`, `*.key`,
  and `*.pem` patterns so these can't be accidentally committed.

## How the Go rewrite applies this

Same rules, enforced structurally:

- External commands go through `internal/execx`, which takes a
  `context.Context` — every external call is cancellable and there is
  one place that constructs `exec.Cmd`, not one per package copying
  the pattern (and potentially the mistakes).
- `internal/lock` provides the concurrency guard `internal/backup`
  must acquire before starting.
- `internal/config` validation runs before `internal/backup` or
  `internal/restore` start doing anything, so a bad repository URL or
  missing password file fails fast with `servervault config validate`
  or `servervault doctor` rather than mid-backup.
- Secrets are read from files, never taken as CLI flags or logged;
  `internal/logger` is expected to never receive a secret value to log
  in the first place, rather than relying on redaction.

## Reporting

See [`SECURITY.md`](../SECURITY.md) for the private vulnerability
reporting process. Do not open a public issue for a security bug.
