package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Event represents a sync lifecycle event.
type Event = string

const (
	SyncStarted Event = "sync.started"
	SyncDone    Event = "sync.done"
	SyncError   Event = "sync.error"
	SyncTest    Event = "sync.test"
)

// Payload is the JSON body sent to the configured webhook URL.
type Payload struct {
	Event      string `json:"event"`
	StackID    string `json:"stack_id"`
	StackName  string `json:"stack_name"`
	SyncLogID  string `json:"sync_log_id"`
	Trigger    string `json:"trigger"`
	CommitSHA  string `json:"commit_sha"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// header is a key/value pair stored in the `headers` JSON field of stack_sync_events.
type header struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Notifier dispatches outgoing webhook notifications for sync job events.
type Notifier struct {
	app    core.App
	client *http.Client
}

// New creates a new Notifier.
func New(app core.App) *Notifier {
	return &Notifier{
		app: app,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Dispatch sends a notification for the given event if the global
// stack_sync_events config is enabled and the event is subscribed.
// It is fire-and-forget: failures are only logged, never returned.
func (n *Notifier) Dispatch(ctx context.Context, p Payload) {
	go func() {
		if err := n.dispatch(ctx, p); err != nil {
			log.Printf("[notify] dispatch %s for stack %s failed: %v", p.Event, p.StackID, err)
		}
	}()
}

// DispatchWithConfig sends a notification using the provided configuration, bypassing db lookup.
// This is useful for testing unpersisted configurations.
func (n *Notifier) DispatchWithConfig(ctx context.Context, cfg *Config, p Payload) error {
	// Always allow test event even if enabled=false
	if p.Event != SyncTest && !cfg.Enabled {
		return nil
	}
	if cfg.Provider == "" {
		cfg.Provider = "webhook"
	}
	provider := NewProvider(n.client, cfg.Provider)
	return provider.Send(cfg, p)
}

func (n *Notifier) dispatch(ctx context.Context, p Payload) error {
	cfg, err := n.LoadConfig()
	if err != nil {
		// No config — silently skip.
		return nil
	}
	if !cfg.Enabled {
		return nil
	}

	provider := NewProvider(n.client, cfg.Provider)
	return provider.Send(cfg, p)
}

func (n *Notifier) LoadConfig() (*Config, error) {
	records, err := n.app.FindAllRecords("stack_sync_events")
	if err != nil || len(records) == 0 {
		return nil, fmt.Errorf("no config")
	}
	rec := records[0]

	cfg := &Config{
		Provider:     rec.GetString("provider"),
		URL:          rec.GetString("url"),
		Secret:       rec.GetString("secret"),
		Enabled:      rec.GetBool("enabled"),
		NtfyUser:     rec.GetString("ntfy_user"),
		NtfyTopic:    rec.GetString("ntfy_topic"),
		NtfyTemplate: rec.GetString("ntfy_template"),
	}

	// Default to webhook if provider is empty
	if cfg.Provider == "" {
		cfg.Provider = "webhook"
	}

	// Parse events multiselect.
	cfg.Events = rec.GetStringSlice("events")

	// Parse headers JSON field.
	rawHeaders := rec.GetString("headers")
	if rawHeaders != "" && rawHeaders != "null" {
		var headers []Header
		if err := json.Unmarshal([]byte(rawHeaders), &headers); err == nil {
			cfg.Headers = headers
		}
	}

	return cfg, nil
}

// MaskSecret returns a masked representation of the secret for API responses.
func MaskSecret(secret string) string {
	if strings.TrimSpace(secret) == "" {
		return ""
	}
	return "••••••••"
}
