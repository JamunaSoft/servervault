# Copilot instructions — ServerVault

These instructions apply to GitHub Copilot (and similar code-completion
assistants) working in this repository. They summarize the fuller
guidance in `CLAUDE.md` and `AGENTS.md`; read those files for details.

## Project

ServerVault is a Linux server backup and disaster recovery toolkit.
`main` holds the stable shell implementation; `go-rewrite` holds the
active Go rewrite. Do not suggest merging incomplete Go work into `main`.

## Safety rules (always apply)

- Never suggest code that deletes a Restic repository automatically.
- Never suggest restoring directly over a live database or live files;
  default to a temporary database / staging directory.
- Never print, log, or hardcode secrets, passwords, tokens, or private
  SSH keys.
- Never build shell commands by concatenating unsanitized input.
- Always support cancellation (`context.Context` in Go, trap/cleanup in
  shell) for long-running or external commands.
- Prevent concurrent backup runs (locking).

## Go conventions

- Target Go 1.22.
- Keep `cmd/servervault` thin; business logic lives under `internal/`.
- Cobra is used only for CLI wiring — command `Run` functions should
  delegate to plain functions that do not import Cobra.
- Use `context.Context` for cancellable operations.
- Wrap errors with `fmt.Errorf("...: %w", err)` and operation context.
- Avoid global mutable state.
- Prefer table-driven tests.
- Use `log/slog` for logging; never log secret values.

## Shell conventions

- Scripts start with `#!/usr/bin/env bash` and `set -Eeuo pipefail`.
- Quote all variable expansions.
- Prefer `bin/*` scripts to remain POSIX-adjacent and ShellCheck-clean.

## Before suggesting a commit is "done"

- Go changes: `gofmt -w .`, `go vet ./...`, `go test ./...`,
  `go build ./cmd/servervault`.
- Shell changes: `bash -n`, `shellcheck`.

## Do not

- Do not invent example hostnames, IPs, or credentials that look real —
  use obvious placeholders (`example.com`, `USER@HOST`, `CHANGEME`).
- Do not implement the Go backup engine ahead of the foundation
  (CLI, config, doctor, logging) described in `PROJECT_STATUS.md`.
