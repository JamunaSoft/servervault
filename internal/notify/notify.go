// Package notify sends optional failure notifications when a job
// terminates unsuccessfully. It has no knowledge of Cobra, backup,
// restore, or retention -- it consumes exactly the same event.Event
// stream those packages already emit through internal/event.Sink,
// via a small Notifier interface, and wraps an existing event.Sink to
// do it (see EventSink) rather than requiring any change to the
// packages that already emit events.
//
// This is the first-party realization of the plugin-readiness guidance
// recorded ahead of time in docs/extensibility.md: Notifier is exactly
// the interface sketched there, and WebhookNotifier is exactly the
// "webhook" implementation that sketch named. A future second channel
// (email, Slack, PagerDuty) is another Notifier implementation, not a
// change to this interface or to EventSink.
package notify

import (
	"context"

	"github.com/JamunaSoft/servervault/internal/event"
)

// Notifier sends one notification for e. Implementations must respect
// ctx (no unbounded blocking) and must not panic on a failure to
// deliver -- return an error instead; EventSink logs and continues
// rather than letting a notification failure affect anything else.
type Notifier interface {
	Notify(ctx context.Context, e event.Event) error
}
