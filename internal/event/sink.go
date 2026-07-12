package event

import (
	"context"
	"sync"
)

// Sink receives emitted events. internal/backup, internal/restore, and
// friends depend on this interface rather than a concrete store, so unit
// tests can substitute NoopSink or InMemorySink without a real database
// -- the same pattern internal/execx.Runner uses for subprocess
// execution.
type Sink interface {
	Emit(ctx context.Context, e Event) error
}

// NoopSink discards every event. It is the safe default for callers that
// haven't configured event persistence -- emitting to it always succeeds
// and never blocks.
type NoopSink struct{}

// Emit implements Sink.
func (NoopSink) Emit(context.Context, Event) error { return nil }

// InMemorySink collects events in memory, for tests. It is safe for
// concurrent use.
type InMemorySink struct {
	mu     sync.Mutex
	events []Event
}

// Emit implements Sink.
func (s *InMemorySink) Emit(_ context.Context, e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	return nil
}

// Events returns a copy of every event emitted so far, in emission order.
func (s *InMemorySink) Events() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Event, len(s.events))
	copy(out, s.events)
	return out
}
