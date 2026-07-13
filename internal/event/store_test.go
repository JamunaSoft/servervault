package event

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/JamunaSoft/servervault/internal/job"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "events.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestOpen_CreatesParentDirectory mirrors internal/job's identical
// regression test -- see that package's doc comment for the real bug
// this guards against.
func TestOpen_CreatesParentDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does", "not", "exist", "yet")
	s, err := Open(filepath.Join(dir, "events.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	e, _ := New(TypeJobCreated, "job-1", SeverityInfo, Metadata{})
	if err := s.Emit(context.Background(), e); err != nil {
		t.Fatalf("Emit after Open into a nonexistent directory tree: %v", err)
	}
}

func TestStore_EmitAndByJob(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	e1, _ := New(TypeJobCreated, "job-1", SeverityInfo, Metadata{})
	e2, _ := New(TypeBackupStarted, "job-1", SeverityInfo, Metadata{SnapshotID: "abc", BytesTotal: 100})
	other, _ := New(TypeJobCreated, "job-2", SeverityInfo, Metadata{})

	for _, e := range []Event{e1, e2, other} {
		if err := s.Emit(ctx, e); err != nil {
			t.Fatalf("Emit: %v", err)
		}
	}

	got, err := s.ByJob(ctx, "job-1")
	if err != nil {
		t.Fatalf("ByJob: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ByJob returned %d events, want 2", len(got))
	}
	if got[0].Type != TypeJobCreated || got[1].Type != TypeBackupStarted {
		t.Errorf("ByJob order/types = %v, %v", got[0].Type, got[1].Type)
	}
	if got[1].Metadata.SnapshotID != "abc" || got[1].Metadata.BytesTotal != 100 {
		t.Errorf("metadata not round-tripped: %+v", got[1].Metadata)
	}
}

func TestStore_Emit_RequiresID(t *testing.T) {
	s := openTestStore(t)
	err := s.Emit(context.Background(), Event{Type: TypeJobCreated})
	if err == nil {
		t.Fatal("Emit with empty ID should fail")
	}
}

func TestStore_ByJob_NoEventsReturnsEmpty(t *testing.T) {
	s := openTestStore(t)
	got, err := s.ByJob(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("ByJob: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ByJob for unknown job = %d events, want 0", len(got))
	}
}

func TestStore_MigrationsSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "events.db")

	s1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	e, _ := New(TypeJobCreated, "job-1", SeverityInfo, Metadata{})
	if err := s1.Emit(context.Background(), e); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open (re-applying migrations would fail here): %v", err)
	}
	defer s2.Close()

	got, err := s2.ByJob(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("ByJob after reopen: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ByJob after reopen = %d events, want 1", len(got))
	}
}

// TestStore_SharesFileWithJobStore proves internal/event.Store and
// internal/job.Store can point at the same SQLite file without their
// independent migration bookkeeping (schema_migrations vs
// event_schema_migrations) colliding -- the design intent from
// docs/core-infrastructure.md.
func TestStore_SharesFileWithJobStore(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "shared.db")

	jobStore, err := job.Open(dbPath)
	if err != nil {
		t.Fatalf("job.Open: %v", err)
	}
	defer jobStore.Close()

	eventStore, err := Open(dbPath)
	if err != nil {
		t.Fatalf("event.Open on the same file: %v", err)
	}
	defer eventStore.Close()

	e, _ := New(TypeJobCreated, "job-1", SeverityInfo, Metadata{})
	if err := eventStore.Emit(context.Background(), e); err != nil {
		t.Fatalf("Emit: %v", err)
	}
}
