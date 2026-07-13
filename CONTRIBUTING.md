# Contributing to ServerVault

Thanks for your interest in improving ServerVault. This document covers
how to set up a development environment, the branch model, and the
checks your change needs to pass before review.

## Branch model

- `main` — stable, production-oriented shell implementation
  (`bin/`, `install.sh`, `systemd/`). Treat it as release-quality.
- `go-rewrite` — active development of the Go CLI (`cmd/`, `internal/`).
  Incomplete Go work stays here; it is not merged into `main` until it
  reaches parity and stability.

Open pull requests against the branch that matches the change:
shell fixes and docs for the stable tool go against `main`; Go
development goes against `go-rewrite`.

## Getting started

```bash
git clone https://github.com/JamunaSoft/servervault.git
cd servervault
git checkout go-rewrite   # for Go development
```

Requirements:

- Go 1.22+
- `gofmt`, `go vet` (ship with Go)
- `shellcheck` (for shell changes)
- `restic`, `zstd`, `postgresql-client` (for running/testing the backup
  tooling locally)

## Making a change

1. Create a topic branch off `main` or `go-rewrite`, as appropriate.
2. Keep changes focused — one logical change per pull request.
3. Follow the design rules in [`CLAUDE.md`](CLAUDE.md) and
   [`AGENTS.md`](AGENTS.md), notably:
   - business logic must not depend on Cobra
   - use `context.Context` for cancellable operations
   - wrap errors with operation context
   - never build shell commands from unsanitized input
   - never commit secrets, hostnames, or credentials
4. Add or update tests for behavior you change.
5. Update relevant docs under `docs/`.

## Required checks before opening a pull request

For Go changes:

```bash
gofmt -w .
go vet ./...
go test ./...
go build ./cmd/servervault
```

For shell changes:

```bash
bash -n bin/* install.sh
shellcheck bin/* install.sh
```

The `go.yml` and `shell.yml` GitHub Actions workflows run the same
checks on every pull request.

## Commit style

Use short, conventional-commit-style messages:

```text
feat(cli): add doctor command
fix(postgres): verify compressed dumps safely
docs: add SFTP example
test(config): cover precedence and validation
```

## Reporting bugs and requesting features

Use the issue templates under `.github/ISSUE_TEMPLATE/`. Please redact
hostnames, credentials, and internal paths from logs before posting.

## Security issues

Do not open a public issue for a security vulnerability. See
[`SECURITY.md`](SECURITY.md) for the reporting process.

## Code of Conduct

By participating in this project you agree to abide by the
[Code of Conduct](CODE_OF_CONDUCT.md).
