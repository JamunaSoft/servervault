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

## 2026-07-11 — Platform architecture proposal + v0.2.0-alpha foundation

- Branch: `go-rewrite`
- What changed: (1) delivered a full architecture proposal for evolving
  ServerVault into a multi-server control-plane/agent platform (Phases
  0–10) as an artifact, approved for Phases 0–1 only; (2) implemented
  `v0.2.0-alpha`'s foundation for real: `internal/version`,
  `internal/execx`, `internal/config` (types, YAML+env loading,
  filesystem-free validation), `internal/logger`, `internal/doctor`
  (7 real checks + 5 explicitly `SKIP`ped checks pending the backup
  engine), and CLI wiring (`servervault doctor`, `servervault config
  validate`, refactored `servervault version`). Every package ships with
  table-driven tests. Fixed the `-X main.Version=...` ldflags in
  `Makefile`/`release.yml` to target `internal/version` now that it
  exists (resolves the TODO left in the prior session). Added
  `docs/threat-model.md` and a platform-roadmap summary table in
  `ROADMAP.md`.
- Decisions / rationale: scoped "Phase 1 foundations" to exactly
  `docs/architecture.md`'s existing "foundation, current milestone" tier
  (`config`, `doctor`, `logger`, plus `version`/`execx` as supporting
  plumbing) and explicitly excluded the backup engine
  (`restic/postgres/mysql/backup/restore/retention/lock/health/notify`)
  — that tier is already labeled "later milestone" in the same diagram,
  and CLAUDE.md says not to start it yet. `doctor` checks that need the
  backup engine (repository access, PostgreSQL connectivity, lock state,
  SSH reachability, systemd/timers) report `StatusSkip` with a reason
  rather than being faked or silently omitted — `Report.Failed()` only
  reacts to `StatusFail`, so skips never block a clean exit code.
  `config.Validate` is deliberately filesystem-free (structural checks
  only); `doctor` owns the I/O-requiring checks (file existence,
  permissions) — this split maps directly to the two separate check
  lists in `CLAUDE.md` ("Config design: Validate" vs. "Doctor command").
  Of the 13 platform docs proposed in the architecture artifact, only
  `docs/threat-model.md` was written now; the rest are deferred to their
  own phases to avoid speculative documentation that drifts from
  not-yet-built code.
- Open questions / follow-ups: `internal/{restic,postgres,mysql,backup,
  restore,retention,lock,health,notify}` are the next milestone
  (`ROADMAP.md` v0.3.0+). The full platform proposal (Phases 2–10) is
  still awaiting a build decision from the user beyond Phases 0–1.
