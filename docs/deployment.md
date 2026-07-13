# Deployment

This covers deploying the stable shell implementation (`main`). The Go
rewrite does not yet have an equivalent installer — see
[`ROADMAP.md`](../ROADMAP.md).

## Supported platforms

Ubuntu 24.04 and Debian-compatible systems with `apt-get`, PostgreSQL,
and systemd. Other distributions are not currently automated by
`install.sh`, though the individual scripts have no distribution-
specific dependencies beyond the packages it installs.

## Install

```bash
git clone https://github.com/JamunaSoft/servervault.git
cd servervault
sudo ./install.sh
```

`install.sh`:

1. Installs `restic`, `zstd`, `postgresql-client`, `openssh-client`,
   and `sudo` via `apt-get`.
2. Creates `/etc/servervault`, `/root/.config/restic`,
   `/var/backups/servervault`, and `/var/restore/servervault`
   (mode `0700`).
3. Installs the example env file and exclude list into
   `/etc/servervault/` (mode `0600`).
4. Installs the three `bin/servervault-*` scripts into
   `/usr/local/sbin/` (mode `0700`).
5. Installs the systemd unit and timer files.
6. Generates a random Restic password into
   `/root/.config/restic/servervault-password` **only if that file
   doesn't already exist or is empty** — it will not overwrite an
   existing password.
7. Enables and starts `servervault-backup.timer` and
   `servervault-verify.timer`.

## After install

1. Edit `/etc/servervault/servervault.env` — see
   [`docs/configuration.md`](configuration.md).
2. Initialize the Restic repository if it doesn't already exist:

   ```bash
   source /etc/servervault/servervault.env
   restic init
   ```

3. Run a backup by hand once to confirm everything is wired up:

   ```bash
   sudo servervault-backup
   ```

4. Confirm the timers are scheduled:

   ```bash
   systemctl list-timers 'servervault-*'
   ```

## Scheduling

- `servervault-backup.timer` — daily at 03:00 local time, with up to a
  5-minute randomized delay (`RandomizedDelaySec=300`) to avoid
  thundering-herd load when multiple hosts share a window.
- `servervault-verify.timer` — runs `restic check --read-data`
  independently of backups, so bit-rot or repository corruption is
  detected even on a day the backup itself doesn't run.

Both services run with `PrivateTmp=true` and `ProtectSystem=full`, and
declare exactly the paths they need write access to
(`ReadWritePaths=`), following the principle of least privilege for
systemd-managed services.

## Uninstalling

`install.sh` has no uninstall counterpart yet. To remove ServerVault by
hand: disable and remove the systemd units, remove the
`/usr/local/sbin/servervault-*` scripts, and decide deliberately
whether to keep or delete `/etc/servervault` and the Restic repository
— ServerVault never deletes a repository automatically, and neither
should you without confirming you no longer need the backups in it.
