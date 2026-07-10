# ServerVault

ServerVault is a production-oriented Linux backup framework built around Restic, PostgreSQL dumps, Zstandard compression, systemd timers, and SFTP-compatible storage such as Hetzner Storage Box.

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

## Security

Never commit repository passwords, SSH private keys, database passwords, or production `.env` files. Store the Restic password in a root-readable file with mode `0600`.

## Disaster recovery

See [docs/DISASTER_RECOVERY.md](docs/DISASTER_RECOVERY.md).

## Acknowledgements

Originally inspired by real-world production backup and disaster-recovery requirements contributed by Sharif Sarkar and Exclusive Education Aid.

## License

MIT
