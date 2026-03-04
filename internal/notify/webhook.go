package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// WebhookProvider implements the Provider interface for generic webhooks.
type WebhookProvider struct {
	client *http.Client
}

// Send dispatches a JSON payload to the configured URL.
// It handles HMAC signing and custom headers.
func (w *WebhookProvider) Send(cfg *Config, p Payload) error {
	// BUGFIX: Always allow sync.test even if not explicitly subscribed
	if p.Event != SyncTest && !cfg.Subscribes(p.Event) {
		return nil
	}
	if cfg.URL == "" {
		return nil
	}

	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "wireops-Notifier/1.0")

	// Custom headers from config (applied before signature so they can't override it).
	for _, h := range cfg.Headers {
		if h.Key != "" {
			req.Header.Set(h.Key, h.Value)
		}
	}

	// HMAC-SHA256 signature.
	if cfg.Secret != "" {
		mac := hmac.New(sha256.New, []byte(cfg.Secret))
		mac.Write(body)
		sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-wireops-Signature", sig)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	log.Printf("[notify] webhook dispatched %s for stack %s → %s (%d)", p.Event, p.StackID, cfg.URL, resp.StatusCode)
	return nil
}
