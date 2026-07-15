package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/JamunaSoft/servervault/internal/event"
)

// defaultTimeout bounds a webhook POST so a slow or unreachable
// endpoint can never hang a caller indefinitely -- notification is a
// side effect, never something the operation being notified about
// should wait on for long.
const defaultTimeout = 10 * time.Second

// WebhookNotifier posts a JSON Payload describing the event to a
// configured URL via HTTP POST -- the first-party Notifier
// implementation, matching NotifyConfig.WebhookURL.
type WebhookNotifier struct {
	url    string
	client *http.Client
}

// NewWebhookNotifier builds a WebhookNotifier posting to url. client
// defaults to one with defaultTimeout if nil.
func NewWebhookNotifier(url string, client *http.Client) *WebhookNotifier {
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	return &WebhookNotifier{url: url, client: client}
}

// Payload is the JSON body WebhookNotifier posts. Every field is
// copied directly from event.Event/event.Metadata's own closed,
// secret-free field set (see internal/event's package doc comment and
// its TestMetadata_NoSecretShapedFields regression guard) -- this type
// adds no field of its own that could carry something those packages
// don't already guarantee is safe to persist and display.
type Payload struct {
	EventType     string    `json:"event_type"`
	Severity      string    `json:"severity"`
	Timestamp     time.Time `json:"timestamp"`
	JobID         string    `json:"job_id"`
	HostRef       string    `json:"host_ref,omitempty"`
	SnapshotID    string    `json:"snapshot_id,omitempty"`
	DatabaseName  string    `json:"database_name,omitempty"`
	PolicyName    string    `json:"policy_name,omitempty"`
	TargetPath    string    `json:"target_path,omitempty"`
	ErrorCategory string    `json:"error_category,omitempty"`
	ErrorSummary  string    `json:"error_summary,omitempty"`
}

// Notify implements Notifier by POSTing a JSON-encoded Payload to the
// configured URL. Any non-2xx response, or a transport-level failure,
// is returned as an error -- EventSink is what decides a notification
// failure never blocks or fails the operation being notified about;
// this method just reports honestly whether delivery succeeded.
func (w *WebhookNotifier) Notify(ctx context.Context, e event.Event) error {
	payload := Payload{
		EventType:     string(e.Type),
		Severity:      string(e.Severity),
		Timestamp:     e.Timestamp,
		JobID:         e.JobID,
		HostRef:       e.HostRef,
		SnapshotID:    e.Metadata.SnapshotID,
		DatabaseName:  e.Metadata.DatabaseName,
		PolicyName:    e.Metadata.PolicyName,
		TargetPath:    e.Metadata.TargetPath,
		ErrorCategory: e.Metadata.ErrorCategory,
		ErrorSummary:  e.Metadata.ErrorSummary,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("notify: webhook: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notify: webhook: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("notify: webhook: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notify: webhook: unexpected status %d", resp.StatusCode)
	}
	return nil
}
