package notify

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/JamunaSoft/servervault/internal/event"
)

// fakeNotifier records every event it's asked to notify about.
type fakeNotifier struct {
	mu     sync.Mutex
	events []event.Event
	err    error
}

func (f *fakeNotifier) Notify(_ context.Context, e event.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return f.err
}

func (f *fakeNotifier) notifiedTypes() []event.Type {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]event.Type, len(f.events))
	for i, e := range f.events {
		out[i] = e.Type
	}
	return out
}

func TestEventSink_Emit_NotifiesOnJobFailed(t *testing.T) {
	underlying := &event.InMemorySink{}
	notifier := &fakeNotifier{}
	sink, err := NewEventSink(underlying, notifier, nil)
	if err != nil {
		t.Fatalf("NewEventSink: %v", err)
	}

	e := testEvent() // event.TypeJobFailed
	if err := sink.Emit(context.Background(), e); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	if len(notifier.notifiedTypes()) != 1 {
		t.Fatalf("notifier was called %d times, want 1", len(notifier.notifiedTypes()))
	}

	// The underlying sink must still have received the event unchanged.
	got := underlying.Events()
	if len(got) != 1 || got[0].ID != e.ID {
		t.Errorf("underlying sink events = %v, want exactly the emitted event", got)
	}
}

func TestEventSink_Emit_DoesNotNotifyOnOtherTypes(t *testing.T) {
	tests := []event.Type{
		event.TypeJobCreated,
		event.TypeJobCancelled,
		event.TypeJobInterrupted,
		event.TypeBackupCompleted,
		event.TypeRetentionCompleted,
	}
	for _, typ := range tests {
		t.Run(string(typ), func(t *testing.T) {
			underlying := &event.InMemorySink{}
			notifier := &fakeNotifier{}
			sink, err := NewEventSink(underlying, notifier, nil)
			if err != nil {
				t.Fatalf("NewEventSink: %v", err)
			}

			e := testEvent()
			e.Type = typ
			if err := sink.Emit(context.Background(), e); err != nil {
				t.Fatalf("Emit: %v", err)
			}

			if len(notifier.notifiedTypes()) != 0 {
				t.Errorf("event type %s: notifier was called, want it left alone (failure notifications only)", typ)
			}
		})
	}
}

func TestEventSink_Emit_NotifierFailureDoesNotFailEmit(t *testing.T) {
	underlying := &event.InMemorySink{}
	notifier := &fakeNotifier{err: errors.New("webhook unreachable")}
	sink, err := NewEventSink(underlying, notifier, nil)
	if err != nil {
		t.Fatalf("NewEventSink: %v", err)
	}

	if err := sink.Emit(context.Background(), testEvent()); err != nil {
		t.Fatalf("Emit should not fail when the notifier fails: %v", err)
	}
	if len(underlying.Events()) != 1 {
		t.Error("the underlying sink must still receive the event even when notification fails")
	}
}

func TestEventSink_Emit_UnderlyingFailureStillNotifies(t *testing.T) {
	// A failure to persist the event locally must not suppress the
	// notification about the job failure it's describing -- these are
	// independent concerns.
	failing := &failingSink{err: errors.New("disk full")}
	notifier := &fakeNotifier{}
	sink, err := NewEventSink(failing, notifier, nil)
	if err != nil {
		t.Fatalf("NewEventSink: %v", err)
	}

	if err := sink.Emit(context.Background(), testEvent()); err == nil {
		t.Fatal("Emit should still surface the underlying sink's own error")
	}
	if len(notifier.notifiedTypes()) != 1 {
		t.Error("notifier should still be called even when the underlying sink fails")
	}
}

type failingSink struct{ err error }

func (f *failingSink) Emit(context.Context, event.Event) error { return f.err }

func TestNewEventSink_RequiresNonNilArgs(t *testing.T) {
	if _, err := NewEventSink(nil, &fakeNotifier{}, nil); err == nil {
		t.Error("NewEventSink with a nil underlying sink should fail")
	}
	if _, err := NewEventSink(&event.InMemorySink{}, nil, nil); err == nil {
		t.Error("NewEventSink with a nil notifier should fail")
	}
}
