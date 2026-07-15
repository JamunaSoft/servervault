package notify

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/JamunaSoft/servervault/internal/event"
)

// EventSink wraps an underlying event.Sink, forwarding every event to
// it unchanged, and additionally calls Notifier.Notify for events that
// represent a job's terminal failure (event.TypeJobFailed) --
// deliberately not TypeJobCancelled (operator-initiated, not a
// failure) and not TypeJobInterrupted (a crash-reconciliation outcome,
// not a live failure to alert on right now). This matches
// NotifyConfig's own doc comment: "optional failure notifications."
//
// A notification failure is logged, never returned from Emit or
// allowed to affect the underlying sink's own result -- the same
// "bookkeeping must never block the operation it's observing" policy
// internal/backup and internal/restore already apply to job/event
// tracking itself. EventSink is purely an additional side effect
// layered on top of an existing event.Sink, satisfying event.Sink
// itself so no caller needs to know it's there.
type EventSink struct {
	underlying event.Sink
	notifier   Notifier
	logger     *slog.Logger
}

// NewEventSink wraps underlying with notifier. Both must be non-nil.
// logger defaults to slog.Default() if nil.
func NewEventSink(underlying event.Sink, notifier Notifier, logger *slog.Logger) (*EventSink, error) {
	if underlying == nil {
		return nil, fmt.Errorf("notify: event sink: underlying sink must not be nil")
	}
	if notifier == nil {
		return nil, fmt.Errorf("notify: event sink: notifier must not be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &EventSink{underlying: underlying, notifier: notifier, logger: logger}, nil
}

// Emit implements event.Sink.
func (s *EventSink) Emit(ctx context.Context, e event.Event) error {
	err := s.underlying.Emit(ctx, e)

	if e.Type == event.TypeJobFailed {
		if notifyErr := s.notifier.Notify(ctx, e); notifyErr != nil {
			s.logger.Warn("notify: failed to send notification", "event_type", string(e.Type), "job_id", e.JobID, "error", notifyErr)
		}
	}

	return err
}
