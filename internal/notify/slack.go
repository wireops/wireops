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
)

// SlackProvider implements notifications through Slack incoming webhooks.
type SlackProvider struct {
	client *http.Client
}

type slackWebhookPayload struct {
	Text   string       `json:"text"`
	Blocks []slackBlock `json:"blocks,omitempty"`
}

type slackBlock struct {
	Type   string      `json:"type"`
	Text   *slackText  `json:"text,omitempty"`
	Fields []slackText `json:"fields,omitempty"`
}

type slackText struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emoji bool   `json:"emoji,omitempty"`
}

// Send dispatches a Slack webhook message with Block Kit content.
func (s *SlackProvider) Send(cfg *Config, p Payload) error {
	if p.Event != SyncTest && !cfg.Subscribes(p.Event) {
		return nil
	}
	if strings.TrimSpace(cfg.URL) == "" {
		return nil
	}

	payload := buildSlackPayload(cfg, p)
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal slack payload: %w", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "wireops-Notifier/1.0")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		msg := strings.TrimSpace(string(responseBody))
		if msg == "" {
			return fmt.Errorf("slack returned status %d", resp.StatusCode)
		}
		return fmt.Errorf("slack returned status %d: %s", resp.StatusCode, msg)
	}

	log.Printf("[notify] slack dispatched %s for stack %s → %s (%d)", p.Event, p.StackID, maskSlackWebhookURL(cfg.URL), resp.StatusCode)
	return nil
}

func buildSlackPayload(cfg *Config, p Payload) slackWebhookPayload {
	title := slackTitle(p)
	mention := ""
	if p.Event == SyncError && cfg.SlackMentionOnError {
		mention = strings.TrimSpace(cfg.SlackMentionText)
	}

	text := title
	if p.StackName != "" {
		text += " for " + p.StackName
	}
	if mention != "" {
		text = mention + " " + text
	}

	blocks := []slackBlock{
		{
			Type: "header",
			Text: &slackText{Type: "plain_text", Text: truncateSlack(title, 150), Emoji: true},
		},
		{
			Type:   "section",
			Fields: slackFields(p),
		},
	}

	if p.Error != "" {
		blocks = append(blocks, slackBlock{
			Type: "section",
			Text: &slackText{Type: "mrkdwn", Text: "*Error:*\n" + truncateSlack(slackEscape(p.Error), 2900)},
		})
	}

	return slackWebhookPayload{
		Text:   truncateSlack(text, 4000),
		Blocks: blocks,
	}
}

func slackTitle(p Payload) string {
	switch p.Event {
	case SyncStarted:
		return "wireops sync started"
	case SyncDone:
		return "wireops sync completed"
	case SyncError:
		return "wireops sync failed"
	case SyncTest:
		return "wireops test notification"
	default:
		return "wireops " + p.Event
	}
}

func slackFields(p Payload) []slackText {
	fields := []slackText{
		{Type: "mrkdwn", Text: "*Stack:*\n" + slackFallback(p.StackName, p.StackID, "unknown")},
		{Type: "mrkdwn", Text: "*Event:*\n" + slackFallback(p.Event, "", "unknown")},
		{Type: "mrkdwn", Text: "*Trigger:*\n" + slackFallback(p.Trigger, "", "unknown")},
	}
	if p.CommitSHA != "" {
		fields = append(fields, slackText{Type: "mrkdwn", Text: "*Commit:*\n" + truncateSlack(slackEscape(p.CommitSHA), 1900)})
	}
	if p.DurationMs > 0 {
		fields = append(fields, slackText{Type: "mrkdwn", Text: fmt.Sprintf("*Duration:*\n%dms", p.DurationMs)})
	}
	if p.SyncLogID != "" {
		fields = append(fields, slackText{Type: "mrkdwn", Text: "*Sync Log:*\n" + truncateSlack(slackEscape(p.SyncLogID), 1900)})
	}
	return fields
}

func slackFallback(primary, secondary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return truncateSlack(slackEscape(primary), 1900)
	}
	if strings.TrimSpace(secondary) != "" {
		return truncateSlack(slackEscape(secondary), 1900)
	}
	return fallback
}

func slackEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	return value
}

func truncateSlack(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}

func maskSlackWebhookURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return MaskSecret(raw)
	}
	u.Path = "/services/..."
	u.RawQuery = ""
	return u.String()
}
