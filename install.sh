#!/usr/bin/env bash
set -Eeuo pipefail
[[ $EUID -eq 0 ]] || { echo "Run as root" >&2; exit 1; }
apt-get update
apt-get install -y restic zstd postgresql-client openssh-client sudo
install -d -m 700 /etc/servervault /root/.config/restic /var/backups/servervault /var/restore/servervault
install -m 600 configs/servervault.env.example /etc/servervault/servervault.env
install -m 600 configs/excludes.txt /etc/servervault/excludes.txt
install -m 700 bin/servervault-backup /usr/local/sbin/servervault-backup
install -m 700 bin/servervault-verify /usr/local/sbin/servervault-verify
install -m 700 bin/servervault-restore /usr/local/sbin/servervault-restore
install -m 644 systemd/servervault-backup.service /etc/systemd/system/servervault-backup.service
install -m 644 systemd/servervault-backup.timer /etc/systemd/system/servervault-backup.timer
install -m 644 systemd/servervault-verify.service /etc/systemd/system/servervault-verify.service
install -m 644 systemd/servervault-verify.timer /etc/systemd/system/servervault-verify.timer
if [[ ! -s /root/.config/restic/servervault-password ]]; then
  openssl rand -base64 48 > /root/.config/restic/servervault-password
  chmod 600 /root/.config/restic/servervault-password
fi
systemctl daemon-reload
systemctl enable --now servervault-backup.timer servervault-verify.timer
printf '\nEdit /etc/servervault/servervault.env, initialize the Restic repository, then run servervault-backup.\n'
