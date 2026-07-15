# Notifications (`internal/notify`)

`internal/notify` sends an optional notification when a backup, restore,
or prune job fails. It is part of `ROADMAP.md`'s v0.5.0 milestone
("Operability"). See
[`docs/extensibility.md`](extensibility.md#notification-channel-plugins)
for the plugin-readiness design this package implements.

## How it's wired in — no changes to backup/restore/retention

`internal/notify` has no dependency on `internal/backup`,
`internal/restore`, or `internal/retention`, and none of those packages
know it exists. It works by wrapping an existing `event.Sink`:

```go
type EventSink struct { /* wraps event.Sink, holds a Notifier */ }

func (s *EventSink) Emit(ctx context.Context, e event.Event) error {
    err := s.underlying.Emit(ctx, e)       // unchanged behavior
    if e.Type == event.TypeJobFailed {     // additional side effect
        s.notifier.Notify(ctx, e)          // failure logged, never returned
    }
    return err
}
```

`internal/cli`'s three commands (`backup`, `restore`, `prune`) each
already open a real `*event.Store` and pass it to their engine/executor
as an `event.Sink`. The only change any of them needed was wrapping
that value — `wrapEventSinkWithNotify(cfg, eventStore, log)` in
`internal/cli/notify.go` — before passing it on. `backup.Engine`,
`restore.Executor`, and `retention.Executor` still just see *an*
`event.Sink`; they have no idea whether it's the plain SQLite store or
one that also notifies.

## What notifies, and what deliberately doesn't

Only `event.TypeJobFailed`. Not `TypeJobCancelled` (operator-initiated
— not a failure to alert on) and not `TypeJobInterrupted` (a crash-
reconciliation outcome recorded after the fact by `Store.Reconcile`,
not a live failure happening right now). This matches `NotifyConfig`'s
own doc comment: "optional **failure** notifications."

## The `Notifier` interface

```go
type Notifier interface {
    Notify(ctx context.Context, e event.Event) error
}
```

`event.Event` is the exact, unchanged type `internal/event` already
defines — a `Notifier` is just another consumer of the same stream
`internal/job`'s own history is built from, not a new data source. A
notification failure is logged and otherwise ignored: it can never fail
or block the backup/restore/prune run that produced the event it's
describing.

## `WebhookNotifier` — the first-party implementation

Posts a small JSON `Payload` to `notify.webhook_url` via HTTP POST,
bounded by a 10-second timeout so an unreachable endpoint can never
hang a run. Every `Payload` field is copied directly from
`event.Event`/`event.Metadata`'s own closed, secret-free field set (see
`internal/event`'s `TestMetadata_NoSecretShapedFields` regression
guard) — this package adds no field of its own that could carry
something those packages don't already guarantee is safe to persist
and display.

```json
{
  "event_type": "job.failed",
  "severity": "error",
  "timestamp": "2026-07-15T12:00:00Z",
  "job_id": "a1b2c3...",
  "host_ref": "srv-1",
  "policy_name": "daily",
  "error_category": "lock",
  "error_summary": "lock: already held by another process"
}
```

## Adding a second channel (email, Slack, PagerDuty, ...)

Implement `Notifier` and register it wherever `NewWebhookNotifier` is
constructed today (`internal/cli/notify.go`), selected by a new
`notify.type`-style config field. No change to `Notifier`, `EventSink`,
or any caller that already emits `event.Event` — exactly the
plugin-readiness this package's design doc committed to in advance.

## Configuration

```yaml
notify:
  enabled: false
  webhook_url: "" # must be http:// or https:// when enabled: true
```

`config.Validate` only checks `webhook_url` when `enabled: true` — a
disabled, empty webhook URL is not an error.
