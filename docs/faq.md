# FAQ

## Which branch should I use?

`main` for a stable, production-ready shell-based backup tool today.
`go-rewrite` if you want to develop the upcoming Go CLI — it is not yet
feature-complete and should not be relied on for production backups
until it reaches the milestones in [`ROADMAP.md`](../ROADMAP.md).

## Does ServerVault support databases other than PostgreSQL?

Not yet. The current implementation (both shell and planned Go) is
PostgreSQL-specific (`pg_dump`/`pg_restore`). See
[`examples/mariadb`](../examples/mariadb) for community-contributed
notes on adapting the approach to MariaDB/MySQL; it's not part of the
core tool yet.

## Does ServerVault support storage backends other than SFTP?

Restic itself supports S3, B2, Azure, GCS, REST servers, and local
paths — ServerVault just passes the `RESTIC_REPOSITORY` URL through, so
any Restic-supported backend works today by setting that variable
accordingly. See [`examples/aws-s3`](../examples/aws-s3) and
[`examples/backblaze-b2`](../examples/backblaze-b2) for backend-specific
configuration notes, and [`examples/hetzner-storagebox`](../examples/hetzner-storagebox)
for the SFTP case the defaults are tuned for.

## Will `servervault-backup` ever delete my Restic repository?

No. See [`docs/security-model.md`](security-model.md) — repository
deletion is never automatic in either implementation.

## Can I restore straight over my production database or files?

Not by default, and not without an explicit manual step. Restores
always land in staging (files) or a temporary database (PostgreSQL)
first — see [`docs/restore-flow.md`](restore-flow.md).

## Where do I put the Restic password and SSH key?

Outside version control, with restrictive permissions. See
[`docs/configuration.md`](configuration.md) and
[`docs/security-model.md`](security-model.md). Never commit them —
`.gitignore` excludes the common filename patterns, but that's a safety
net, not a substitute for keeping them out of the repo in the first
place.

## `servervault-backup` says "Another backup is already running" but nothing is running. What do I do?

Check for a stale lock holder (`lsof /run/lock/servervault-backup.lock`
or `fuser`); if the previous run genuinely crashed without releasing
the lock, it's safe to remove the lock file once you've confirmed no
process holds it. This is intentionally not automated — see
[`docs/security-model.md`](security-model.md) on why concurrent backups
are refused rather than worked around silently.

## How do I verify a repository without running a backup?

`servervault-verify` (or, on the Go side, the planned `servervault
verify`) runs `restic check --read-data` independently of the backup
schedule. See [`docs/deployment.md`](deployment.md).

## I found a security issue. Where do I report it?

Not as a public GitHub issue — see [`SECURITY.md`](../SECURITY.md).

## How do I contribute?

See [`CONTRIBUTING.md`](../CONTRIBUTING.md).
