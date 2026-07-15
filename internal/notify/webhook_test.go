package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/JamunaSoft/servervault/internal/event"
)

func testEvent() event.Event {
	return event.Event{
		ID:        "evt-1",
		Type:      event.TypeJobFailed,
		Timestamp: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
		JobID:     "job-1",
		HostRef:   "srv-1",
		Severity:  event.SeverityError,
		Metadata: event.Metadata{
			SnapshotID:    "abc123",
			PolicyName:    "daily",
			ErrorCategory: "execution",
			ErrorSummary:  "restic backup: connection refused",
		},
	}
}

func TestWebhookNotifier_Notify_PostsJSONPayload(t *testing.T) {
	var gotMethod, gotContentType string
	var gotPayload Payload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL, nil)
	if err := n.Notify(context.Background(), testEvent()); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotPayload.EventType != string(event.TypeJobFailed) {
		t.Errorf("EventType = %q, want %q", gotPayload.EventType, event.TypeJobFailed)
	}
	if gotPayload.JobID != "job-1" {
		t.Errorf("JobID = %q, want job-1", gotPayload.JobID)
	}
	if gotPayload.SnapshotID != "abc123" {
		t.Errorf("SnapshotID = %q, want abc123", gotPayload.SnapshotID)
	}
	if gotPayload.ErrorSummary != "restic backup: connection refused" {
		t.Errorf("ErrorSummary = %q, want the configured error summary", gotPayload.ErrorSummary)
	}
}

func TestWebhookNotifier_Notify_NonSuccessStatusIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL, nil)
	if err := n.Notify(context.Background(), testEvent()); err == nil {
		t.Fatal("Notify against a 500-returning endpoint: want an error, got nil")
	}
}

func TestWebhookNotifier_Notify_UnreachableURLIsError(t *testing.T) {
	n := NewWebhookNotifier("http://127.0.0.1:0", nil)
	if err := n.Notify(context.Background(), testEvent()); err == nil {
		t.Fatal("Notify against an unreachable URL: want an error, got nil")
	}
}

func TestWebhookNotifier_Notify_RespectsCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	n := NewWebhookNotifier(srv.URL, nil)
	if err := n.Notify(ctx, testEvent()); err == nil {
		t.Fatal("Notify with an already-cancelled context: want an error, got nil")
	}
}
