package cli

import (
	"log/slog"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/event"
	"github.com/JamunaSoft/servervault/internal/notify"
)

// wrapEventSinkWithNotify wraps sink with notify.EventSink when
// notify.enabled is true in cfg, so a job.failed event additionally
// posts to the configured webhook -- shared by backup/restore/prune's
// CLI wiring rather than duplicated in each. Returns sink unchanged
// when notifications aren't configured, or if wiring them up
// unexpectedly fails (logged, never fatal -- notification is a side
// effect, the same "must never block the operation it's observing"
// policy internal/notify.EventSink itself applies one level down).
func wrapEventSinkWithNotify(cfg *config.Config, sink event.Sink, log *slog.Logger) event.Sink {
	if !cfg.Notify.Enabled {
		return sink
	}
	notifier := notify.NewWebhookNotifier(cfg.Notify.WebhookURL, nil)
	wrapped, err := notify.NewEventSink(sink, notifier, log)
	if err != nil {
		log.Warn("notify: failed to wire up notifications; continuing without them", "error", err)
		return sink
	}
	return wrapped
}
