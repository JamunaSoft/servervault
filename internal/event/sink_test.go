package event

import (
	"context"
	"sync"
	"testing"
)

func TestNoopSink_AlwaysSucceeds(t *testing.T) {
	e, _ := New(TypeJobCreated, "job-1", SeverityInfo, Metadata{})
	if err := (NoopSink{}).Emit(context.Background(), e); err != nil {
		t.Errorf("NoopSink.Emit returned error: %v", err)
	}
}

func TestInMemorySink_CollectsInOrder(t *testing.T) {
	s := &InMemorySink{}
	ctx := context.Background()

	types := []Type{TypeJobCreated, TypeJobStarted, TypeBackupStarted, TypeBackupCompleted}
	for _, typ := range types {
		e, _ := New(typ, "job-1", SeverityInfo, Metadata{})
		if err := s.Emit(ctx, e); err != nil {
			t.Fatalf("Emit: %v", err)
		}
	}

	got := s.Events()
	if len(got) != len(types) {
		t.Fatalf("Events() returned %d events, want %d", len(got), len(types))
	}
	for i, typ := range types {
		if got[i].Type != typ {
			t.Errorf("event %d type = %s, want %s", i, got[i].Type, typ)
		}
	}
}

func TestInMemorySink_EventsReturnsACopy(t *testing.T) {
	s := &InMemorySink{}
	e, _ := New(TypeJobCreated, "job-1", SeverityInfo, Metadata{})
	_ = s.Emit(context.Background(), e)

	got := s.Events()
	got[0].Type = "mutated"

	got2 := s.Events()
	if got2[0].Type == "mutated" {
		t.Error("Events() did not return an independent copy -- mutating the result affected internal state")
	}
}

func TestInMemorySink_ConcurrentEmit(t *testing.T) {
	s := &InMemorySink{}
	ctx := context.Background()

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			e, _ := New(TypeJobCreated, "job-1", SeverityInfo, Metadata{})
			_ = s.Emit(ctx, e)
		}()
	}
	wg.Wait()

	if got := len(s.Events()); got != n {
		t.Errorf("Events() returned %d events, want %d", got, n)
	}
}
