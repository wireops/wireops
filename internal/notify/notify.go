package notify

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
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

// Notifier dispatches outgoing notifications for sync job events.
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

// Dispatch sends a notification for the given event to all enabled notification integrations.
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
	return provider.Send(ctx, cfg, p)
}

func (n *Notifier) dispatch(ctx context.Context, p Payload) error {
	recs, err := n.app.FindAllRecords("integrations", dbx.HashExp{"enabled": true})
	if err != nil {
		return err
	}

	for _, rec := range recs {
		slug := rec.GetString("slug")
		if slug != "webhook" && slug != "ntfy" && slug != "discord" && slug != "slack" {
			continue
		}

		var configMap map[string]interface{}
		if err := rec.UnmarshalJSONField("config", &configMap); err != nil {
			log.Printf("[notify] failed to unmarshal integration %s config: %v", slug, err)
			continue
		}

		cfg := n.BuildConfig(slug, configMap)
		if cfg == nil {
			continue
		}

		if !cfg.Subscribes(p.Event) {
			continue
		}

		provider := NewProvider(n.client, cfg.Provider)
		if err := provider.Send(ctx, cfg, p); err != nil {
			log.Printf("[notify] dispatch to %s failed: %v", slug, err)
		}
	}

	return nil
}

// BuildConfig maps database JSON config map to the local Config struct.
func (n *Notifier) BuildConfig(slug string, configMap map[string]interface{}) *Config {
	cfg := &Config{
		Provider: slug,
		Enabled:  true,
	}

	if eventsRaw, ok := configMap["events"].([]interface{}); ok {
		for _, e := range eventsRaw {
			if eStr, ok := e.(string); ok {
				cfg.Events = append(cfg.Events, eStr)
			}
		}
	}

	if urlVal, ok := configMap["url"].(string); ok {
		cfg.URL = urlVal
	}

	if secretVal, ok := configMap["secret"].(string); ok {
		cfg.Secret = secretVal
	}

	if slug == "webhook" {
		if headersRaw, ok := configMap["headers"].([]interface{}); ok {
			for _, h := range headersRaw {
				if hMap, ok := h.(map[string]interface{}); ok {
					key, _ := hMap["key"].(string)
					val, _ := hMap["value"].(string)
					if key != "" {
						cfg.Headers = append(cfg.Headers, Header{Key: key, Value: val})
					}
				}
			}
		}
	} else if slug == "ntfy" {
		if userVal, ok := configMap["user"].(string); ok {
			cfg.NtfyUser = userVal
		}
		if topicVal, ok := configMap["topic"].(string); ok {
			cfg.NtfyTopic = topicVal
		}
		if templateVal, ok := configMap["template"].(string); ok {
			cfg.NtfyTemplate = templateVal
		}
	} else if slug == "discord" {
		if usernameVal, ok := configMap["username"].(string); ok {
			cfg.DiscordUsername = usernameVal
		}
		if avatarVal, ok := configMap["avatar_url"].(string); ok {
			cfg.DiscordAvatarURL = avatarVal
		}
		if mentionVal, ok := configMap["mention_on_error"].(bool); ok {
			cfg.DiscordMentionOnError = mentionVal
		}
		if roleVal, ok := configMap["role_id"].(string); ok {
			cfg.DiscordRoleID = roleVal
		}
	} else if slug == "slack" {
		if mentionVal, ok := configMap["mention_on_error"].(bool); ok {
			cfg.SlackMentionOnError = mentionVal
		}
		if mentionTextVal, ok := configMap["mention_text"].(string); ok {
			cfg.SlackMentionText = mentionTextVal
		}
	}

	return cfg
}

// MaskSecret returns a masked representation of the secret for API responses.
func MaskSecret(secret string) string {
	if strings.TrimSpace(secret) == "" {
		return ""
	}
	return "••••••••"
}
