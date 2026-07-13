# Disaster recovery

## Goal

Restore a Linux server after disk loss, accidental deletion, or
provider failure, without making the situation worse by restoring over
data that turns out to still be needed.

## Before disaster strikes

Keep at least two offline copies of each of the following, away from
the server they protect:

- Restic repository password
- SSH private key used to reach the repository (if using SFTP)
- Restic repository address
- Database role/connection information
- DNS and hosting-provider account recovery codes

None of these belong in this repository. See
[`docs/security-model.md`](security-model.md) for how ServerVault
avoids ever needing them in git history.

## High-level workflow

1. Provision a fresh Ubuntu/Debian server.
2. Install PostgreSQL, the application stack, Restic, Zstandard, and an
   OpenSSH client — `install.sh` handles the ServerVault-specific
   dependencies (`restic zstd postgresql-client openssh-client sudo`).
3. Restore the Restic password file and SSH private key from your
   offline secret store, with `0600` permissions.
4. Clone ServerVault and configure `/etc/servervault/servervault.env`
   (see [`docs/configuration.md`](configuration.md)) or, for the Go
   rewrite, `configs/servervault.example.yaml`.
5. Run `servervault-restore` and restore the latest snapshot into a
   safe (staging) directory — see
   [`docs/restore-flow.md`](restore-flow.md).
6. Verify the restored application files and the PostgreSQL dump
   before trusting them.
7. Restore the database into a **temporary** database, not production.
8. Validate the temporary database, then switch application
   configuration over only after validation passes.
9. Re-enable services, TLS, DNS, and monitoring; confirm
   `servervault doctor` (or `servervault-verify`) is clean before
   considering the incident closed.

## PostgreSQL safety

Do not restore directly into the production database. Restore into a
temporary database first, validate, and cut over deliberately:

```bash
sudo -u postgres createdb app_restore_test
sudo -u postgres pg_restore --clean --if-exists --no-owner \
  --dbname=app_restore_test /path/to/app_production.dump
```

## Repository safety

If `restic check` reports errors on the repository you're restoring
from, stop and investigate before pruning or writing new snapshots to
it — ServerVault refuses destructive cleanup on a repository that
fails validation (see [`docs/security-model.md`](security-model.md));
do the same by hand if you're operating on the repository directly
with `restic`.

## After recovery

- Rotate any credential that may have been exposed during the incident
  (e.g. if the original host's disk is unrecoverable but not
  confirmed wiped).
- Run a fresh backup once the recovered host is stable, so the
  recovery itself is captured in the snapshot history.
- Record what happened and what you'd change next time — see
  [`AI_MEMORY.md`](../AI_MEMORY.md) if an AI agent assisted with the
  recovery and learned something worth keeping for next time.
