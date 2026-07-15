# ServerVault

ServerVault is a production-oriented Linux backup framework built around Restic, PostgreSQL dumps, Zstandard compression, systemd timers, and SFTP-compatible storage such as Hetzner Storage Box.

> **Current stable implementation:** shell (`main`) — production-ready, this is what `install.sh` installs.
> **Go rewrite:** experimental (`go-rewrite`, `v0.3.0-alpha`) — pre-release, not yet at feature parity, not recommended for production. See [Branches](#branches) below.

## Features

- PostgreSQL custom-format dumps compressed with Zstandard
- Restic encryption, deduplication, snapshots, and retention
- Direct SFTP repository support
- Daily, weekly, and monthly retention
- Repository verification
- Safe restore workflow
- systemd services and timers
- ShellCheck CI

## Quick start

```bash
git clone https://github.com/JamunaSoft/servervault.git
cd servervault
sudo ./install.sh
sudo nano /etc/servervault/servervault.env
sudo servervault-backup
```

## Default retention

- 7 daily snapshots
- 4 weekly snapshots
- 12 monthly snapshots

## Supported environment

Initial release targets Ubuntu 24.04 and Debian-compatible systems using PostgreSQL and Restic. Other databases and storage backends are planned.

## Branches

This repository carries two implementations side by side while the Go rewrite is in progress:

- **`main`** — the stable, production-oriented shell implementation described above (`bin/`, `install.sh`, `systemd/`). This is what `install.sh` installs today. It receives bug fixes only; new features land in the Go rewrite instead.
- **`go-rewrite`** — active development of a Go CLI (`cmd/servervault`, `internal/`) intended to eventually replace the shell implementation. `servervault version`/`doctor`/`config validate`/`backup`/`snapshots`/`restore` are implemented, tested, and merged. A policy-driven retention engine (`servervault prune`) and optional webhook failure notifications are implemented on `feature/retention-v0.5.0` pending review; `status` and health checks are not yet implemented — do not rely on this branch for production backups. See [`ROADMAP.md`](ROADMAP.md) for milestone status and [`docs/architecture.md`](docs/architecture.md) for how the two implementations relate.

Incomplete Go work is never merged into `main`. If you just want a working backup tool, use `main`.

## Security

Never commit repository passwords, SSH private keys, database passwords, or production `.env` files. Store the Restic password in a root-readable file with mode `0600`.

## Disaster recovery

See [docs/disaster-recovery.md](docs/disaster-recovery.md).

## Documentation

- [Architecture](docs/architecture.md)
- [Configuration](docs/configuration.md)
- [Deployment](docs/deployment.md)
- [Backup flow](docs/backup-flow.md)
- [Restore flow](docs/restore-flow.md)
- [Security model](docs/security-model.md)
- [Development](docs/development.md)
- [Testing](docs/testing.md)
- [FAQ](docs/faq.md)

## Acknowledgements

Originally inspired by real-world production backup and disaster-recovery requirements contributed by Sharif Sarkar and Exclusive Education Aid.

## License

MIT
