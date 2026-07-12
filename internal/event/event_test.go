package event

import (
	"reflect"
	"strings"
	"testing"
)

func TestNew_AssignsIDAndTimestamp(t *testing.T) {
	e, err := New(TypeBackupStarted, "job-1", SeverityInfo, Metadata{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if e.ID == "" {
		t.Error("New did not assign an ID")
	}
	if e.Timestamp.IsZero() {
		t.Error("New did not assign a timestamp")
	}
	if e.Timestamp.Location().String() != "UTC" {
		t.Errorf("New timestamp location = %v, want UTC", e.Timestamp.Location())
	}
	if e.JobID != "job-1" {
		t.Errorf("JobID = %q, want %q", e.JobID, "job-1")
	}
}

func TestNew_DistinctIDsAcrossCalls(t *testing.T) {
	e1, _ := New(TypeJobCreated, "job-1", SeverityInfo, Metadata{})
	e2, _ := New(TypeJobCreated, "job-1", SeverityInfo, Metadata{})
	if e1.ID == e2.ID {
		t.Errorf("two calls to New produced the same ID %q", e1.ID)
	}
}

// TestMetadata_NoSecretShapedFields is a regression guard: it fails if
// anyone ever adds a field to Metadata whose name suggests it could hold
// a credential. Metadata's entire safety argument (see event.go's doc
// comment) rests on it being a closed set of known-safe fields with no
// generic escape hatch -- see the identical guard in internal/job.
func TestMetadata_NoSecretShapedFields(t *testing.T) {
	denylist := []string{"password", "secret", "token", "key", "credential", "env"}

	typ := reflect.TypeOf(Metadata{})
	for i := 0; i < typ.NumField(); i++ {
		name := strings.ToLower(typ.Field(i).Name)
		for _, bad := range denylist {
			if strings.Contains(name, bad) {
				t.Errorf("Metadata field %q looks secret-shaped (matches %q) -- events must never persist this", typ.Field(i).Name, bad)
			}
		}
	}

	eventType := reflect.TypeOf(Event{})
	for i := 0; i < eventType.NumField(); i++ {
		f := eventType.Field(i)
		if f.Type.Kind() == reflect.Map {
			t.Errorf("Event field %q is a map -- event.Metadata must stay a closed, typed struct, not gain a free-form map escape hatch", f.Name)
		}
	}
}

// TestEventSink_HasNoMutationMethods is a structural check that Sink
// (and, by extension, every implementation including the SQLite-backed
// Store) exposes exactly Emit -- no Update, no Delete -- proving the
// "append-only operational record" contract from the package doc comment
// at the interface level, not just by convention in Store's
// implementation.
func TestEventSink_HasNoMutationMethods(t *testing.T) {
	sinkType := reflect.TypeOf((*Sink)(nil)).Elem()
	if sinkType.NumMethod() != 1 {
		t.Fatalf("Sink interface has %d methods, want exactly 1 (Emit) -- append-only contract requires no mutation methods", sinkType.NumMethod())
	}
	if sinkType.Method(0).Name != "Emit" {
		t.Fatalf("Sink's only method is %q, want %q", sinkType.Method(0).Name, "Emit")
	}
}
