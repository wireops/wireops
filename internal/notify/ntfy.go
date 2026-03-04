package notify

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"text/template"
)

// NtfyProvider implements the Provider interface for ntfy.sh notifications.
type NtfyProvider struct {
	client *http.Client
}

// Send dispatches a notification to the configured ntfy server/topic.
// It supports custom templates, basic auth, and sets appropriate headers (Title, Priority, Tags).
func (n *NtfyProvider) Send(cfg *Config, p Payload) error {
	// BUGFIX: Always allow sync.test even if not explicitly subscribed
	if p.Event != SyncTest && !cfg.Subscribes(p.Event) {
		return nil
	}
	if cfg.URL == "" {
		return nil
	}
	if cfg.NtfyTopic == "" {
		// Topic is required for ntfy
		return nil
	}

	// Construct the full URL: base URL + topic
	url := strings.TrimRight(cfg.URL, "/") + "/" + cfg.NtfyTopic

	// Determine body content
	var body bytes.Buffer
	if cfg.NtfyTemplate != "" {
		tmpl, err := template.New("ntfy").Parse(cfg.NtfyTemplate)
		if err != nil {
			return fmt.Errorf("parse template: %w", err)
		}
		if err := tmpl.Execute(&body, p); err != nil {
			return fmt.Errorf("execute template: %w", err)
		}
	} else {
		// Default format
		if p.Event == SyncTest {
			fmt.Fprintf(&body, "Test notification from wireops for stack %s", p.StackName)
		} else {
			emoji := "✅"
			if p.Event == SyncError {
				emoji = "🚨"
			} else if p.Event == SyncStarted {
				emoji = "🚀"
			}
			fmt.Fprintf(&body, "%s Event: %s\nStack: %s\nTrigger: %s\nCommit: %s", emoji, p.Event, p.StackName, p.Trigger, p.CommitSHA)
			if p.Error != "" {
				fmt.Fprintf(&body, "\nError: %s", p.Error)
			}
			if p.DurationMs > 0 {
				fmt.Fprintf(&body, "\nDuration: %dms", p.DurationMs)
			}
		}
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, &body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "wireops-Notifier/1.0")

	// Set ntfy headers based on event
	title := fmt.Sprintf("wireops: %s - %s", p.Event, p.StackName)
	tags := "wireops,docker"
	priority := "default"

	switch p.Event {
	case SyncStarted:
		tags += ",rocket"
	case SyncDone:
		tags += ",white_check_mark"
	case SyncError:
		tags += ",rotating_light,error"
		priority = "high"
	case SyncTest:
		tags += ",test_tube"
		title = "wireops: Test Notification"
	}

	req.Header.Set("Title", title)
	req.Header.Set("Tags", tags)
	req.Header.Set("Priority", priority)

	// Basic Auth
	if cfg.NtfyUser != "" || cfg.Secret != "" {
		req.SetBasicAuth(cfg.NtfyUser, cfg.Secret)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("ntfy returned status %d", resp.StatusCode)
	}

	log.Printf("[notify] ntfy dispatched %s for stack %s → %s (%d)", p.Event, p.StackID, url, resp.StatusCode)
	return nil
}
