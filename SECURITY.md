# Security Policy

## Supported versions

ServerVault is pre-1.0 software. Security fixes are applied to the
latest release on `main` (stable shell implementation) and, where
applicable, backported to the most recent `go-rewrite` milestone.

| Version / branch | Supported |
| ----------------- | --------- |
| `main` (latest tag) | Yes |
| `go-rewrite` (pre-release) | Best effort |
| Older tagged releases | No |

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security
vulnerabilities. Instead, use GitHub's private vulnerability reporting:

1. Go to the repository's **Security** tab.
2. Select **Report a vulnerability**.
3. Include steps to reproduce, affected version/commit, and impact.

If private reporting is unavailable, contact the maintainers listed in
[`.github/CODEOWNERS`](.github/CODEOWNERS) directly rather than filing
a public issue.

We aim to acknowledge reports within 5 business days.

## Scope

In scope:

- The `servervault-*` shell scripts and `install.sh`
- The Go CLI under `cmd/` and `internal/`
- systemd unit files under `systemd/`
- GitHub Actions workflows under `.github/workflows/`

Out of scope:

- Vulnerabilities in third-party dependencies (`restic`, PostgreSQL,
  `zstd`, OpenSSH) — please report those upstream.
- Issues that require an attacker to already have root access to the
  target host.

## Handling secrets

ServerVault is designed around the following invariants; a report that
shows one of these is violated is treated as a security bug:

- Restic repository passwords, SSH private keys, and database
  credentials are never logged, printed, or committed.
- Backup and restore scripts never interpolate untrusted input directly
  into shell commands.
- Restores never overwrite a live database or live files by default.
- Repository deletion is never automatic.

See [`docs/security-model.md`](docs/security-model.md) for the full
threat model and design rationale.
