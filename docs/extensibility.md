# Extensibility: storage backends and notification channels

**Status:** design only — see [`control-plane-architecture.md`](control-plane-architecture.md)
for how this fits the wider platform. Nothing below is implemented.

This document isn't one of the four `ROADMAP.md` pre-named for the
platform phases (`control-plane-architecture.md`, `agent-architecture.md`,
`api-design.md`, `data-model.md`). It's added because the requirements
driving this design pass named two extensibility points explicitly
("plugin-ready storage backends," "plugin-ready notification channels")
that don't cleanly belong inside any of those four. It follows the same
convention: design only, no code.

## What "plugin" means here

**A plugin is a Go type, compiled into the same binary, registered by
name at init time — not a dynamically loaded `.so`/`.dll` and not
third-party/untrusted code executed at runtime.** Go's own `plugin`
package is platform-limited (no Windows, fragile across Go versions)
and, more importantly, wrong for this project's trust model: `CLAUDE.md`
and `docs/release-process.md` already establish that ServerVault is
"trusted to run as root against production." Loading arbitrary
dynamically-linked code into that process would be a new, severe attack
surface with no equivalent today. The registry pattern below is the same
one Go's own standard library uses for `database/sql` drivers and image
codecs: every implementation ships in the same module, in the same
binary, reviewed the same way as everything else — "plugin-ready" means
*adding a new implementation is cheap and doesn't touch the caller*, not
*a stranger's code can run in this process*.

```go
// Illustrative shape, not implementation.
type Factory[T any] func(config map[string]any) (T, error)

func Register[T any](registry map[string]Factory[T], name string, f Factory[T])
```

A `Policy.backend_type` or `NotificationChannel.channel_type` string
(see [`data-model.md`](data-model.md)) selects a registered factory by
name. Adding a new backend or channel is: write the Go type, register it
by name, done — no change to `internal/backup`, `internal/restore`, or
the API.

## Storage backend plugins

### What's already solved, and stays solved

Restic itself already multiplexes storage backends through its own
repository string — `local:`, `sftp:`, `s3:`, `b2:`, `azure:`, `gs:`,
`rest:`. `ROADMAP.md`'s v0.3.5 milestone already made the correct call
here: *"a general storage abstraction (Restic already abstracts storage
backends directly)"* was deliberately left out of scope. This document
does not reopen that decision. If the only requirement were "back up to
different storage providers," nothing needs to change — that's a
`restic.repository` config string today and always will be.

### What actually needs a plugin point

The thing genuinely worth making pluggable is the **backup engine
itself** — Restic vs. some future alternative (a different snapshot
tool, a cloud-native backup API, etc.), not the storage underneath a
single engine. `internal/restic.Repository` already exposes exactly the
method set a plugin interface needs, because it was designed narrowly
and consistently (`Backup`, `Check`, `Snapshots`, `CatConfig`, `Restore`,
`Stats`, `List` — see `ROADMAP.md`'s v0.3.0/v0.4.0-alpha.1 entries for
the exact set, deliberately *not* including `Init`/`Forget`/`Prune`/
`Unlock`, which stay absent here too).

```go
// Illustrative shape, mirroring internal/restic.Repository's existing
// method set exactly. Not implementation.
type Backend interface {
    Backup(ctx context.Context, opts BackupOptions) (BackupSummary, error)
    Check(ctx context.Context) error
    Snapshots(ctx context.Context) ([]Snapshot, error)
    Restore(ctx context.Context, opts RestoreOptions) (RestoreSummary, error)
    Stats(ctx context.Context, snapshotID string) (Stats, error)
    List(ctx context.Context, snapshotID, path string) ([]FileInfo, error)
}
```

`internal/restic.Repository` becomes the `"restic"` registered
implementation of this interface via a **thin adapter added alongside
it**, not a modification of the package itself — `internal/restic`
keeps its own doc comment's promise ("wraps the real restic CLI... never
a Go library") completely intact. `internal/backup.Engine` and
`internal/restore.Planner`/`Executor` already depend on interfaces
(`ResticClient` and equivalents, per their existing test-fake pattern)
rather than the concrete `*restic.Repository` type — so this adapter
satisfies an interface they already accept, no change to those packages
either. This is `control-plane-architecture.md`'s package-reuse table
made concrete for this one case.

### Registration

```yaml
# Illustrative config shape, not implementation.
repositories:
  - name: primary
    backend_type: restic     # registry key; "restic" is the only
                              # first-party implementation today
    repository: "sftp:host:path"
```

## Notification channel plugins

`internal/notify` doesn't exist yet — it's `ROADMAP.md`'s v0.5.0
milestone ("optional failure notifications"). This section is guidance
for building it plugin-ready **from its first line of code**, so v0.5.0
doesn't have to be revisited when a second channel type is added later.
It does not change v0.5.0's scope or move its milestone.

```go
// Illustrative shape for the eventual internal/notify package.
// Not implementation, and not a change to any existing package.
type Notifier interface {
    Notify(ctx context.Context, e event.Event) error
}
```

`event.Event` is the exact, unchanged type from `internal/event` (see
`data-model.md`'s compatibility contract) — a `Notifier` is just another
consumer of the same append-only event stream the control plane
forwards, not a new data source.

The config surface already has the first registered implementation's
shape sketched today:

```go
// internal/config/config.go, today, unchanged:
type NotifyConfig struct {
    Enabled    bool
    WebhookURL string
}
```

`"webhook"` is the first-party `Notifier` registered against this
config shape at v0.5.0. Later channels (email, Slack, PagerDuty) are
additional registered implementations selected by
`NotificationChannel.channel_type` (`data-model.md`), each with its own
config shape — none of them require a change to the `Notifier`
interface or to any caller that already emits `event.Event`.

## Non-goals

- No dynamically loaded code, ever, in-process. A plugin is a Go file in
  this repository, compiled in, reviewed like any other change.
- No third-party or untrusted plugin execution. If ServerVault ever
  wants genuinely external, sandboxed extensions, that is a different
  project with a different threat model — out of scope here, and would
  need its own `threat-model.md` update before it could be considered,
  the same way the platform work itself did.
- No plugin marketplace, discovery mechanism, or versioned plugin API
  contract. "Plugin-ready" in this document means low-friction to add a
  new first-party implementation, not a stable ABI for external authors.
