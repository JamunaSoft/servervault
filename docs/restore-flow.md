# Restore flow

This describes the `servervault-restore` shell script (`main`). The Go
rewrite's `servervault restore` (planned) follows the same
staging-first, temp-database-first defaults through `internal/restore`.

## Guiding rule

**Restores never overwrite live data by default.** Files are restored
into a new, timestamped directory; databases are restored into a new,
temporary database. Promoting a restore to "live" is a separate,
explicit, human step that this tool does not perform automatically.

## Menu

Running `servervault-restore` presents:

1. **List snapshots** — `restic snapshots --tag=servervault`. Read-only.
2. **Restore latest snapshot to a safe directory** — creates
   `$RESTORE_ROOT/latest-<timestamp>/` and restores into it.
3. **Restore a specific snapshot to a safe directory** — same as (2),
   but prompts for a snapshot ID first.
4. **Extract latest PostgreSQL dump** — restores only the `*.dump.zst`
   file from the latest snapshot into
   `$RESTORE_ROOT/database-<timestamp>/`, decompresses it, and runs
   `pg_restore --list` against it to confirm the archive is valid
   before declaring success.

Every restore target is a fresh, uniquely named directory under
`RESTORE_ROOT` — nothing is restored in place.

## Restoring the database for real

Extracting the dump (menu option 4) only verifies and stages it. To
apply it, restore into a **new** database first, exactly as in the
[disaster recovery guide](disaster-recovery.md):

```bash
sudo -u postgres createdb app_restore_test
sudo -u postgres pg_restore --clean --if-exists --no-owner \
  --dbname=app_restore_test /path/to/extracted.dump
```

Validate the restored data in `app_restore_test`, then cut the
application over to it (rename, or repoint the connection string) only
once you're satisfied — ServerVault does not do this step for you.

## Restoring files for real

Compare the staged restore directory against the live path (`diff -rq`,
or review the specific files you need) and copy over only what you've
validated. Do not `rsync --delete` a staged restore straight onto a
live path without review.

## Go rewrite notes

`internal/restore` is expected to expose the same two primitives —
restore-to-staging for files, restore-to-temp-database for PostgreSQL —
as library functions, so `servervault restore` can offer the same
interactive flow plus a non-interactive mode for scripted DR drills.
