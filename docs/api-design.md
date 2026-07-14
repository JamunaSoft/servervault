# API design

**Status:** design only — see [`control-plane-architecture.md`](control-plane-architecture.md)
for how this fits the wider platform. Nothing below is implemented; no
endpoint listed here exists.

## The API is the only backend the web UI has

Restated from `control-plane-architecture.md` because it's the
organizing principle of this document: the web UI has no code path into
`internal/`, no direct database access beyond the control-plane DB
through this API, and no way to reach a server except through that
server's enrolled agent. Every capability the web UI ever offers is
something this API already exposes to the CLI and the agent first.
Concretely, that means **every endpoint is designed for at least two of
these three callers**, and the design is wrong if it isn't:

| Caller | How it uses the API |
| --- | --- |
| Agent | Polls for pending jobs, reports status/events, authenticates via the enrollment-issued ed25519 key (signed requests) |
| CLI, in remote/fleet mode | A human running `servervault` against a server they don't have a shell on, authenticating as themselves |
| Web UI | Same endpoints the CLI's remote mode uses, authenticating as a logged-in human session |

There is no "web-only" endpoint. If the web UI needs it, the CLI can do
it too — this is the concrete mechanism behind "CLI remains first-class
forever": the CLI is never a second-class client of a capability that
only the web UI has.

## Versioning and shape

- Path-versioned: `/api/v1/...`. Breaking changes get a new version
  prefix; `v1` is additive-only once it ships (new optional fields, new
  endpoints — never a removed or repurposed field), matching
  `internal/job.Metadata`'s own "add a new named field, never repurpose
  one" discipline.
- JSON request/response bodies. Agent requests are additionally signed
  (see below); human requests carry a session or API token.
- Every mutating endpoint requires an authenticated, attributable actor
  and produces an audit event — `threat-model.md`'s Repudiation
  mitigation ("every mutating API call requires an authenticated actor
  and writes an audit event"), not optional per-endpoint.

## Authentication, by caller

Reusing `threat-model.md`'s already-decided mechanisms — this section
maps them onto the API surface, it doesn't re-decide them:

- **Agents**: every request signed with the ed25519 key generated at
  enrollment (`agent-architecture.md`). The control plane verifies the
  signature against that agent's registered public key. No password, no
  bearer token that could leak from a log.
- **Humans (CLI remote mode or web session)**: Argon2id-hashed
  credentials, lockout/rate-limiting per account+IP, optional TOTP —
  exactly `threat-model.md`'s Spoofing/"Account takeover" row. A human
  session issues a short-lived token the CLI or browser attaches to
  subsequent requests; exact token format is an implementation detail
  for `authentication.md` (deliberately not written yet, per
  `control-plane-architecture.md`'s "deliberately not designed here").

## Resource model

Endpoint groups map directly onto [`data-model.md`](data-model.md)'s
entities — this table is the index, not the full contract:

| Resource | Represents | Typical operations |
| --- | --- | --- |
| `/organizations` | `Organization` (future; see data-model.md) | list, get — no self-serve create until Phase 6 |
| `/servers` | `Server` (an enrolled agent) | list, get, get-status (last-seen, last job) |
| `/repositories` | `Repository` (a Restic repository reference, no secrets) | list, get, create, update |
| `/policies` | `Policy` (one backup configuration, scheduled or manual) | list, get, create, update, disable |
| `/jobs` | Control-plane mirror of a server's `internal/job.Job` rows | list (filterable by server/policy/type/state), get, **create** (submit) |
| `/jobs/{id}/events` | Control-plane mirror of `internal/event.Event` rows for that job | list |
| `/snapshots` | Read-through to a repository's Restic snapshot list (via the owning agent) | list, get |
| `/notification-channels` | `NotificationChannel` (see `extensibility.md`) | list, get, create, update, test |
| `/storage-backends` | Introspection of registered backend plugins (see `extensibility.md`) | list (names + capabilities only) |
| `/agent/poll` | Agent-only: long-poll for pending jobs assigned to the caller's server | agent GET |
| `/agent/report` | Agent-only: push job/event updates | agent POST |

## Job submission is a closed vocabulary

This is the API design decision `threat-model.md` already forces:
*"Elevation of privilege: Web panel runs an arbitrary command — no such
code path exists by design; the job protocol is a closed, typed enum,
not a command string."* Concretely, `POST /jobs` accepts exactly one of
a fixed set of typed request shapes — the same three the CLI already
exposes as flags, and the same three `internal/job.Type` already
enumerates (`TypeBackup`, `TypeRestore`, `TypePrune`):

```go
// Illustrative shape, not implementation. One of these three, never a
// free-form command.
type BackupJobRequest struct {
    PolicyID string
}

type RestoreJobRequest struct {
    PolicyID   string
    SnapshotID string
    Target     string // "files" | "temp-db", mirrors restore.Target
    Path       string // optional, mirrors --path
    Database   string // optional, mirrors --database
    DryRun     bool
}

type PruneJobRequest struct {
    PolicyID string
}
```

There is no field anywhere in this vocabulary that carries a shell
string, a file path outside what `Planner`/`Executor` already validate,
or an arbitrary argv. The agent that eventually executes this request
does so by calling `Engine.Run`/`Executor.Execute` with the equivalent
Go struct — the same validation, staging, and revalidation logic the
CLI's own flag parsing feeds into today. A `RestoreJobRequest` cannot
express "restore over the live database" any more than
`servervault restore --target temp-db` can, because it's consumed by
the identical `Executor`.

## Read models: mirrors, not queries into the agent

`/jobs`, `/jobs/{id}/events`, and `/servers/{id}` are served entirely
from the control-plane database — they are populated by what the agent
has already reported (`agent-architecture.md`'s forwarding), not a
live query the API makes into a running agent. This keeps the API
responsive when a server is offline (you can still see its last-known
job history) and keeps the "control plane never opens a connection into
an agent" invariant from `control-plane-architecture.md` absolute, with
one narrow, explicit exception:

- `/snapshots` is a **read-through**: the control plane asks the owning
  agent (via the same poll channel — a "please report your current
  snapshot list" item in the agent's next poll response, not a new
  inbound connection) because a repository's true snapshot list only
  ever lives in Restic itself, and mirroring it into the control-plane
  DB would create a second, potentially stale source of truth for data
  `internal/restic.Repository.Snapshots` already knows how to list
  correctly. This does not violate pull-only: the agent still initiates
  every connection.

## What this document does not specify

Exact endpoint request/response JSON schemas, pagination format, rate
limits, and error envelope shape are implementation detail for whichever
phase actually builds this API (Phase 3 per `ROADMAP.md`) — pinning them
now, before a single handler exists, is exactly the speculative drift
`control-plane-architecture.md`'s "deliberately not designed here"
section warns against. What's fixed here is the *shape of the contract*
(closed job vocabulary, mirror-not-query read model, every caller is a
first-class API client) — that shape is what the rest of the platform
is designed against.
