# AI Memory Log

This file is a running handoff log for AI coding agents (Claude Code,
Copilot, Codex, Kilo Code, etc.) working on ServerVault across sessions.
It is a supplement to — not a replacement for — `CLAUDE.md`, `AGENTS.md`,
and `PROJECT_STATUS.md`, which hold the durable instructions and current
status. Use this file for things that don't belong in those: decisions
made, dead ends explored, and context that would otherwise be lost
between sessions.

## How to use this file

- **Read first.** Before starting work, skim the most recent entries.
- **Append, don't rewrite.** Add a new dated entry per session; do not
  delete or rewrite prior entries unless they are actively wrong.
- **No secrets.** Never record hostnames, credentials, tokens, private
  IPs, or production paths here — this file is committed to git. Use
  placeholders (`HOST`, `USER`, `example.com`) the same way the rest of
  the repository does.
- **Be concrete.** Prefer "chose X over Y because Z" to vague status
  updates — the goal is to save the next agent from re-deriving a
  decision that was already made.

## Entry format

```markdown
## YYYY-MM-DD — short title

- Branch:
- What changed:
- Decisions / rationale:
- Open questions / follow-ups:
```

## Log

## 2026-07-11 — Repository scaffolding

- Branch: `go-rewrite`
- What changed: filled in previously empty placeholder files across
  `.github/`, `.vscode/`, `configs/`, and `docs/` — CI workflows,
  issue/PR templates, editor config, example configs, and
  documentation. No changes to Go source or shell scripts beyond fixing
  a stale `config/` path reference in `install.sh` after the
  `config/ -> configs/` rename.
- Decisions / rationale: kept scope to documentation/tooling scaffolding
  per `CLAUDE.md`'s "current priority" — the Go backup engine is
  explicitly deferred until the CLI/config/doctor/logging foundation is
  complete.
- Open questions / follow-ups: `internal/config`, `internal/doctor`,
  `internal/logger`, and friends are still empty packages; that is the
  next milestone (see `ROADMAP.md` v0.2.0-alpha).
