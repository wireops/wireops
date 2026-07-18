package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	discordColorStarted = 0x3498db
	discordColorDone    = 0x2ecc71
	discordColorError   = 0xe74c3c
	discordColorTest    = 0x95a5a6
)

// DiscordProvider implements notifications through Discord incoming webhooks.
type DiscordProvider struct {
	client *http.Client
}

type discordWebhookPayload struct {
	Content         string                 `json:"content,omitempty"`
	Username        string                 `json:"username,omitempty"`
	AvatarURL       string                 `json:"avatar_url,omitempty"`
	Embeds          []discordEmbed         `json:"embeds"`
	AllowedMentions discordAllowedMentions `json:"allowed_mentions"`
}

type discordAllowedMentions struct {
	Parse []string `json:"parse"`
	Roles []string `json:"roles,omitempty"`
}

type discordEmbed struct {
	Title       string              `json:"title"`
	Description string              `json:"description,omitempty"`
	Color       int                 `json:"color"`
	Fields      []discordEmbedField `json:"fields,omitempty"`
	Footer      discordEmbedFooter  `json:"footer"`
	Timestamp   string              `json:"timestamp"`
}

type discordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type discordEmbedFooter struct {
	Text string `json:"text"`
}

// Send dispatches a Discord webhook message with an embed summarizing the sync event.
func (d *DiscordProvider) Send(ctx context.Context, cfg *Config, p Payload) error {
	if p.Event != SyncTest && !cfg.Subscribes(p.Event) {
		return nil
	}
	if strings.TrimSpace(cfg.URL) == "" {
		return nil
	}
	if err := ValidateProviderURL("discord", cfg.URL); err != nil {
		return err
	}

	payload := buildDiscordPayload(cfg, p)
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal discord payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, discordWebhookURL(cfg.URL), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "wireops-Notifier/1.0")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		msg := strings.TrimSpace(string(responseBody))
		if msg == "" {
			return fmt.Errorf("discord returned status %d", resp.StatusCode)
		}
		return fmt.Errorf("discord returned status %d: %s", resp.StatusCode, msg)
	}

	log.Printf("[notify] discord dispatched %s for stack %s → %s (%d)", p.Event, p.StackID, maskDiscordWebhookURL(cfg.URL), resp.StatusCode)
	return nil
}

func buildDiscordPayload(cfg *Config, p Payload) discordWebhookPayload {
	content := ""
	allowed := discordAllowedMentions{Parse: []string{}}
	if (p.Event == SyncError || p.Event == BackupMirrorError) && cfg.DiscordMentionOnError && strings.TrimSpace(cfg.DiscordRoleID) != "" {
		roleID := strings.TrimSpace(cfg.DiscordRoleID)
		content = "<@&" + roleID + ">"
		allowed.Roles = []string{roleID}
	}

	return discordWebhookPayload{
		Content:   content,
		Username:  strings.TrimSpace(cfg.DiscordUsername),
		AvatarURL: strings.TrimSpace(cfg.DiscordAvatarURL),
		Embeds: []discordEmbed{{
			Title:       discordTitle(p),
			Description: truncateDiscord(p.Error, 2048),
			Color:       discordColor(p.Event),
			Fields:      discordFields(p),
			Footer:      discordEmbedFooter{Text: "wireops"},
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		}},
		AllowedMentions: allowed,
	}
}

func discordWebhookURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("wait", "true")
	u.RawQuery = q.Encode()
	return u.String()
}

func discordTitle(p Payload) string {
	switch p.Event {
	case SyncStarted:
		return "Sync started"
	case SyncDone:
		return "Sync completed"
	case SyncError:
		return "Sync failed"
	case BackupMirrorError:
		return "Backup mirror failed"
	case SyncTest:
		return "Test notification"
	default:
		return p.Event
	}
}

func discordColor(event string) int {
	switch event {
	case SyncStarted:
		return discordColorStarted
	case SyncDone:
		return discordColorDone
	case SyncError, BackupMirrorError:
		return discordColorError
	case SyncTest:
		return discordColorTest
	default:
		return discordColorTest
	}
}

func discordFields(p Payload) []discordEmbedField {
	fields := []discordEmbedField{
		{Name: "Stack", Value: fallbackDiscordValue(p.StackName, p.StackID, "unknown"), Inline: true},
		{Name: "Event", Value: fallbackDiscordValue(p.Event, "", "unknown"), Inline: true},
		{Name: "Trigger", Value: fallbackDiscordValue(p.Trigger, "", "unknown"), Inline: true},
	}
	if p.CommitSHA != "" {
		fields = append(fields, discordEmbedField{Name: "Commit", Value: truncateDiscord(p.CommitSHA, 1024), Inline: true})
	}
	if p.DurationMs > 0 {
		fields = append(fields, discordEmbedField{Name: "Duration", Value: fmt.Sprintf("%dms", p.DurationMs), Inline: true})
	}
	if p.SyncLogID != "" {
		fields = append(fields, discordEmbedField{Name: "Sync Log", Value: truncateDiscord(p.SyncLogID, 1024), Inline: true})
	}
	return fields
}

func fallbackDiscordValue(primary, secondary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return truncateDiscord(primary, 1024)
	}
	if strings.TrimSpace(secondary) != "" {
		return truncateDiscord(secondary, 1024)
	}
	return fallback
}

func truncateDiscord(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}

func maskDiscordWebhookURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return MaskSecret(raw)
	}
	u.Path = "/api/webhooks/..."
	u.RawQuery = ""
	return u.String()
}
