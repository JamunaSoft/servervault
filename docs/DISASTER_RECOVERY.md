# Disaster Recovery

## Goal

Restore a Linux server after disk loss, accidental deletion, or provider failure.

## High-level workflow

1. Provision a fresh Ubuntu or Debian server.
2. Install PostgreSQL, the web stack, Restic, Zstandard, and OpenSSH client.
3. Restore the Restic password file and SSH private key from an offline secret store.
4. Clone ServerVault and configure `/etc/servervault/servervault.env`.
5. Run `servervault-restore` and restore into a safe directory.
6. Verify application files and the PostgreSQL dump.
7. Restore the database into a temporary database first.
8. Switch application configuration only after validation.
9. Re-enable services, TLS, DNS, and monitoring.

## PostgreSQL safety

Do not overwrite the production database immediately. Restore into a temporary database first:

```bash
sudo -u postgres createdb app_restore_test
sudo -u postgres pg_restore --clean --if-exists --no-owner \
  --dbname=app_restore_test /path/to/app_production.dump
```

## Secrets

The Restic repository is unusable without its password. Keep at least two offline copies of:

- Restic password
- SSH private key
- repository address
- database role information
- DNS and provider access recovery codes
