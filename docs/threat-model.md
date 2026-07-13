# Threat model

This extends [`docs/security-model.md`](security-model.md) — which covers
the shell implementation and the Go CLI foundation that exists today — with
the additional threats introduced once the platform work (control plane,
agents, multi-tenancy) begins. See the architecture proposal referenced
from [`ROADMAP.md`](../ROADMAP.md) for the full design; this document is
the security-focused summary that's expected to stay accurate as that work
lands, phase by phase.

**Status:** the "today" section below is implemented and enforced by tests.
The "platform" section describes threats a *future* network surface will
introduce — nothing in that section exists yet (no control plane, no
agent, no multi-tenancy — see [`ROADMAP.md`](../ROADMAP.md)'s Phase 1
status). It is recorded now, ahead of that code, so the security posture is
designed before the surface exists rather than retrofitted after.

## Today: CLI and shell implementation

Unchanged from [`docs/security-model.md`](security-model.md):

| Threat | Mitigation |
| --- | --- |
| Accidental data loss by the operator | Safe-by-default actions: staging-first restore, temp-DB-first restore, no automatic repository deletion |
| Credential exposure | Secrets read from files, never logged, never passed as CLI flags or committed |
| Command injection | External commands invoked as argv slices (`internal/execx`), never through a shell string |
| Concurrent execution | `flock` in the shell implementation; `internal/lock` planned for the Go engine (ROADMAP v0.3.0) |
| Silent corruption | Dumps verified before trust; `restic check` after every prune |

Enforced today, specifically by the Phase 1 foundation:

- `internal/config.Validate` rejects a `restore.staging_root` that equals a
  live `backup.paths` entry, and a `restore.temp_database_prefix` that
  equals the live `postgres.database` — both checked with tests
  (`internal/config/validate_test.go`), not just documented.
- `internal/doctor` checks secret file permissions (rejects group/world
  readable password files) and required-command availability
  non-destructively, before any backup/restore is attempted.
- `internal/execx.Run` takes command name and arguments as separate
  parameters (never a shell string), making command injection structurally
  unavailable rather than merely discouraged.

## Platform (not yet built): additional threats

Once a control plane and agents exist, the network surface grows and so
does the threat list. Summarized from the architecture proposal's threat
model (STRIDE-organized):

| Category | Threat | Planned mitigation |
| --- | --- | --- |
| Spoofing | Fake or stolen agent identity | Short-lived single-use enrollment tokens; per-agent ed25519 keys; signed requests |
| Spoofing | Account takeover | Argon2id hashing, lockout/rate limiting, optional TOTP |
| Tampering | Job payload modified in transit | TLS + control-plane-signed job payloads, verified independently by the agent |
| Tampering | Audit log altered after the fact | Append-only table; no `UPDATE`/`DELETE` grant for the application DB role |
| Repudiation | Action with no attributable actor | Every mutating API call requires an authenticated actor and writes an audit event |
| Information disclosure | Cross-tenant data access | Mandatory tenant-scoped repository queries + an automated cross-tenant test suite |
| Information disclosure | Secrets leaking via logs/API/UI | Write-only secret fields; encryption at rest for control-plane-held credentials |
| Denial of service | Compromised agent floods the job queue | Per-agent rate limits, per-server concurrency caps |
| Denial of service | Login brute-forcing | Exponential lockout per account+IP |
| Elevation of privilege | Viewer triggers a backup/restore | `backup:*`/`restore:*` permissions checked and tested separately |
| Elevation of privilege | Web panel runs an arbitrary command | No such code path exists by design — the job protocol is a closed, typed enum, not a command string |

## What does not change

Regardless of how much control-plane surface gets added, the following
stay true by design, not by policy:

1. The agent's job executor only ever calls typed Go functions from the
   backup engine — there is no code path from "message arrived over the
   network" to "shell command runs."
2. A restore never targets a live path or a live database by default, at
   any layer — engine, job vocabulary, or (once built) approval workflow.
3. A Restic repository is never deleted automatically, at any layer.

## Reporting

See [`SECURITY.md`](../SECURITY.md) for the private vulnerability reporting
process.
