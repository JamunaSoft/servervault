# Backup flow

This describes the `servervault-backup` shell script (`main`). The Go
rewrite's `servervault backup` (planned, see
[`ROADMAP.md`](../ROADMAP.md)) will follow the same flow through
`internal/backup`, `internal/postgres`, and `internal/restic`.

## Steps

1. **Load configuration.** Source `/etc/servervault/servervault.env`
   (or `$SERVERVAULT_CONFIG`). Abort if it is missing or unreadable.
2. **Acquire the lock.** Take an exclusive `flock` on
   `/run/lock/servervault-backup.lock`. If another backup is running,
   exit immediately rather than queueing or overlapping — see
   [`docs/security-model.md`](security-model.md) for why concurrent
   backups are refused rather than serialized.
3. **Preflight.** Confirm `restic`, `pg_dump`, `pg_restore`, and `zstd`
   are on `PATH`, and that PostgreSQL is reachable
   (`SELECT 1` round-trip).
4. **Dump the database.** `pg_dump --format=custom --no-owner
   --no-privileges`, piped directly into `zstd` (streaming; the
   uncompressed dump never touches disk).
5. **Verify the dump before it is trusted.** Test the Zstandard frame
   (`zstd -t`), decompress to a temporary file, and confirm
   `pg_restore --list` can read it. The temporary file is removed
   immediately after.
6. **Back up files and the dump together.** A single `restic backup`
   call covers the configured `BACKUP_PATHS` plus the verified dump,
   tagged with `servervault` and the server's host tag, and using the
   configured exclude file.
7. **Apply retention.** `restic forget --keep-daily --keep-weekly
   --keep-monthly --prune`, scoped to the current host tag so multiple
   hosts sharing a repository don't prune each other's snapshots.
8. **Verify the repository.** `restic check` after pruning, so a
   corrupted repository is caught the same run it happened, not on the
   next scheduled `servervault-verify`.
9. **Report.** Print the five most recent snapshots for the host.
10. **Clean up.** A `trap ... EXIT` removes the local compressed dump
    file regardless of success or failure, so backup runs don't
    accumulate local disk usage.

## Failure behavior

Every step uses `set -Eeuo pipefail`, so the script stops at the first
failing command — a failed dump never gets backed up as if it
succeeded, and a failed `restic backup` never gets pruned as if it
completed. Nothing here deletes the Restic repository itself; `forget
--prune` only removes snapshots outside the retention window, and only
after the backup step succeeded.

## Scheduling

`systemd/servervault-backup.timer` runs the backup once daily
(`OnCalendar=*-*-* 03:00:00`) with a randomized delay, via
`systemd/servervault-backup.service`. Independently,
`servervault-verify.timer` runs `restic check --read-data` on its own
schedule — see [`docs/deployment.md`](deployment.md).
