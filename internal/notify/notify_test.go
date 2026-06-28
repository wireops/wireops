package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type captureTransport struct {
	requests []*http.Request
	bodies   [][]byte
}

func (c *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	_ = req.Body.Close()
	c.requests = append(c.requests, req.Clone(req.Context()))
	c.bodies = append(c.bodies, body)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("ok")),
		Request:    req,
	}, nil
}

// TestWebhookProviderSend verifies the webhook implementation.
func TestWebhookProviderSend(t *testing.T) {
	var received struct {
		method  string
		body    []byte
		headers http.Header
	}
	transport := &captureTransport{}

	provider := &WebhookProvider{
		client: &http.Client{Transport: transport},
	}

	cfg := &Config{
		Provider: "webhook",
		URL:      "http://webhook.local/sync",
		Secret:   "supersecret",
		Events:   []string{SyncDone},
		Headers:  []Header{{Key: "X-Custom", Value: "hello"}},
		Enabled:  true,
	}

	p := Payload{
		Event:      SyncDone,
		StackID:    "stack-abc",
		DurationMs: 1234,
	}

	if err := provider.Send(cfg, p); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(transport.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(transport.requests))
	}
	received.method = transport.requests[0].Method
	received.headers = transport.requests[0].Header
	received.body = transport.bodies[0]

	// Verify method
	if received.method != http.MethodPost {
		t.Errorf("expected POST, got %s", received.method)
	}

	// Verify payload JSON
	var got Payload
	if err := json.Unmarshal(received.body, &got); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if got.Event != SyncDone {
		t.Errorf("event: got %q, want %q", got.Event, SyncDone)
	}

	// Verify HMAC signature
	sig := received.headers.Get("X-wireops-Signature")
	if !strings.HasPrefix(sig, "sha256=") {
		t.Errorf("X-wireops-Signature missing or malformed: %q", sig)
	}

	// Verify custom header
	if received.headers.Get("X-Custom") != "hello" {
		t.Errorf("X-Custom header: got %q, want %q", received.headers.Get("X-Custom"), "hello")
	}
}

// TestWebhookProviderEventFiltering verifies subscription logic + test event bypass.
func TestWebhookProviderEventFiltering(t *testing.T) {
	transport := &captureTransport{}

	provider := &WebhookProvider{client: &http.Client{Transport: transport}}
	cfg := &Config{
		URL:     "http://webhook.local/sync",
		Events:  []string{SyncDone},
		Enabled: true,
	}

	// Unsubscribed event -> no call
	if err := provider.Send(cfg, Payload{Event: SyncError}); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if got := len(transport.requests); got != 0 {
		t.Errorf("expected 0 calls for unsubscribed event, got %d", got)
	}

	// Subscribed event -> call
	if err := provider.Send(cfg, Payload{Event: SyncDone}); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if got := len(transport.requests); got != 1 {
		t.Errorf("expected 1 call for subscribed event, got %d", got)
	}

	// Test event (not in list) -> call (bypass)
	if err := provider.Send(cfg, Payload{Event: SyncTest}); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if got := len(transport.requests); got != 2 {
		t.Errorf("expected 2 calls after sync.test, got %d", got)
	}
}

// TestNtfyProviderSend verifies ntfy integration.
func TestNtfyProviderSend(t *testing.T) {
	var received struct {
		path    string
		headers http.Header
		body    string
		auth    string
	}
	transport := &captureTransport{}

	provider := &NtfyProvider{
		client: &http.Client{Transport: transport},
	}

	cfg := &Config{
		Provider:  "ntfy",
		URL:       "http://ntfy.local",
		NtfyTopic: "mytopic",
		NtfyUser:  "user",
		Secret:    "pass",
		Events:    []string{SyncError},
		Enabled:   true,
	}

	// Test error event (high priority)
	p := Payload{
		Event:     SyncError,
		StackName: "prod-stack",
		Error:     "deploy failed",
	}

	if err := provider.Send(cfg, p); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(transport.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(transport.requests))
	}
	received.path = transport.requests[0].URL.Path
	received.headers = transport.requests[0].Header
	received.body = string(transport.bodies[0])
	received.auth = transport.requests[0].Header.Get("Authorization")

	// Verify URL path construction
	if received.path != "/mytopic" {
		t.Errorf("expected path /mytopic, got %s", received.path)
	}

	// Verify headers
	if received.headers.Get("Priority") != "high" {
		t.Errorf("expected Priority: high, got %s", received.headers.Get("Priority"))
	}
	if !strings.Contains(received.headers.Get("Tags"), "error") {
		t.Errorf("expected Tags to contain error, got %s", received.headers.Get("Tags"))
	}

	// Verify auth (Basic base64(user:pass))
	if !strings.HasPrefix(received.auth, "Basic ") {
		t.Errorf("expected Basic auth, got %s", received.auth)
	}

	// Verify body contains error message
	if !strings.Contains(received.body, "deploy failed") {
		t.Errorf("body missing error message: %s", received.body)
	}
}

// TestNtfyProviderTemplate verifies custom template rendering.
func TestNtfyProviderTemplate(t *testing.T) {
	transport := &captureTransport{}

	provider := &NtfyProvider{client: &http.Client{Transport: transport}}
	cfg := &Config{
		URL:          "http://ntfy.local",
		NtfyTopic:    "topic",
		NtfyTemplate: "Hello {{.StackName}}, status is {{.Event}}",
		Events:       []string{SyncDone},
	}

	p := Payload{
		Event:     SyncDone,
		StackName: "world",
	}

	if err := provider.Send(cfg, p); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(transport.bodies) != 1 {
		t.Fatalf("requests = %d, want 1", len(transport.bodies))
	}

	expected := "Hello world, status is sync.done"
	body := string(transport.bodies[0])
	if body != expected {
		t.Errorf("expected body %q, got %q", expected, body)
	}
}

func TestDiscordProviderSend(t *testing.T) {
	transport := &captureTransport{}

	provider := &DiscordProvider{client: &http.Client{Transport: transport}}
	cfg := &Config{
		Provider:              "discord",
		URL:                   "https://discord.com/api/webhooks/123/token",
		Events:                []string{SyncError},
		DiscordUsername:       "wireops-test",
		DiscordMentionOnError: true,
		DiscordRoleID:         "987654321",
	}

	p := Payload{
		Event:      SyncError,
		StackID:    "stack-abc",
		StackName:  "prod-stack",
		Trigger:    "manual",
		CommitSHA:  "abc1234",
		DurationMs: 2500,
		Error:      "deploy failed",
	}

	if err := provider.Send(cfg, p); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(transport.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(transport.requests))
	}
	req := transport.requests[0]
	if req.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", req.Method)
	}
	if req.URL.Query().Get("wait") != "true" {
		t.Errorf("expected wait=true query, got %q", req.URL.RawQuery)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("expected application/json, got %q", got)
	}

	var got struct {
		Content  string `json:"content"`
		Username string `json:"username"`
		Embeds   []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Color       int    `json:"color"`
			Fields      []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"fields"`
		} `json:"embeds"`
		AllowedMentions struct {
			Parse []string `json:"parse"`
			Roles []string `json:"roles"`
		} `json:"allowed_mentions"`
	}
	if err := json.Unmarshal(transport.bodies[0], &got); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if got.Username != "wireops-test" {
		t.Errorf("username = %q, want wireops-test", got.Username)
	}
	if got.Content != "<@&987654321>" {
		t.Errorf("content = %q, want role mention", got.Content)
	}
	if len(got.AllowedMentions.Parse) != 0 {
		t.Errorf("expected no broad allowed mention parsing, got %v", got.AllowedMentions.Parse)
	}
	if len(got.AllowedMentions.Roles) != 1 || got.AllowedMentions.Roles[0] != "987654321" {
		t.Errorf("roles = %v, want [987654321]", got.AllowedMentions.Roles)
	}
	if len(got.Embeds) != 1 {
		t.Fatalf("embeds = %d, want 1", len(got.Embeds))
	}
	if got.Embeds[0].Title != "Sync failed" {
		t.Errorf("title = %q, want Sync failed", got.Embeds[0].Title)
	}
	if got.Embeds[0].Description != "deploy failed" {
		t.Errorf("description = %q, want deploy failed", got.Embeds[0].Description)
	}
	if got.Embeds[0].Color != discordColorError {
		t.Errorf("color = %d, want %d", got.Embeds[0].Color, discordColorError)
	}

	var sawStack bool
	for _, field := range got.Embeds[0].Fields {
		if field.Name == "Stack" && field.Value == "prod-stack" {
			sawStack = true
			break
		}
	}
	if !sawStack {
		t.Errorf("expected Stack field for prod-stack, got %+v", got.Embeds[0].Fields)
	}
}

func TestDiscordProviderEventFiltering(t *testing.T) {
	transport := &captureTransport{}

	provider := &DiscordProvider{client: &http.Client{Transport: transport}}
	cfg := &Config{
		URL:    "https://discord.com/api/webhooks/123/token",
		Events: []string{SyncDone},
	}

	if err := provider.Send(cfg, Payload{Event: SyncError}); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if got := len(transport.requests); got != 0 {
		t.Errorf("expected 0 calls for unsubscribed event, got %d", got)
	}

	if err := provider.Send(cfg, Payload{Event: SyncTest}); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if got := len(transport.requests); got != 1 {
		t.Errorf("expected 1 call after sync.test, got %d", got)
	}

	var body struct {
		AllowedMentions struct {
			Parse []string `json:"parse"`
			Roles []string `json:"roles"`
		} `json:"allowed_mentions"`
	}
	if err := json.Unmarshal(transport.bodies[0], &body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if len(body.AllowedMentions.Parse) != 0 || len(body.AllowedMentions.Roles) != 0 {
		t.Errorf("expected safe empty allowed_mentions, got %+v", body.AllowedMentions)
	}
}

func TestSlackProviderSend(t *testing.T) {
	transport := &captureTransport{}

	provider := &SlackProvider{client: &http.Client{Transport: transport}}
	cfg := &Config{
		Provider:            "slack",
		URL:                 "https://hooks.slack.com/services/T000/B000/token",
		Events:              []string{SyncError},
		SlackMentionOnError: true,
		SlackMentionText:    "<!subteam^S123456|deploys>",
	}

	p := Payload{
		Event:      SyncError,
		StackID:    "stack-abc",
		StackName:  "prod <stack>",
		Trigger:    "manual",
		CommitSHA:  "abc1234",
		DurationMs: 2500,
		Error:      "deploy failed <bad>",
	}

	if err := provider.Send(cfg, p); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(transport.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(transport.requests))
	}
	req := transport.requests[0]
	if req.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", req.Method)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("expected application/json, got %q", got)
	}

	var got struct {
		Text   string `json:"text"`
		Blocks []struct {
			Type string `json:"type"`
			Text struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"text"`
			Fields []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"fields"`
		} `json:"blocks"`
	}
	if err := json.Unmarshal(transport.bodies[0], &got); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if !strings.HasPrefix(got.Text, "<!subteam^S123456|deploys> wireops sync failed") {
		t.Errorf("text = %q, want mention prefix", got.Text)
	}
	if len(got.Blocks) != 3 {
		t.Fatalf("blocks = %d, want 3", len(got.Blocks))
	}
	if got.Blocks[0].Type != "header" || got.Blocks[0].Text.Text != "wireops sync failed" {
		t.Errorf("header block = %+v", got.Blocks[0])
	}

	var sawEscapedStack bool
	for _, field := range got.Blocks[1].Fields {
		if strings.Contains(field.Text, "prod &lt;stack&gt;") {
			sawEscapedStack = true
			break
		}
	}
	if !sawEscapedStack {
		t.Errorf("expected escaped stack in fields, got %+v", got.Blocks[1].Fields)
	}
	if !strings.Contains(got.Blocks[2].Text.Text, "deploy failed &lt;bad&gt;") {
		t.Errorf("expected escaped error block, got %q", got.Blocks[2].Text.Text)
	}
}

func TestSlackProviderEventFiltering(t *testing.T) {
	transport := &captureTransport{}

	provider := &SlackProvider{client: &http.Client{Transport: transport}}
	cfg := &Config{
		URL:    "https://hooks.slack.com/services/T000/B000/token",
		Events: []string{SyncDone},
	}

	if err := provider.Send(cfg, Payload{Event: SyncError}); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if got := len(transport.requests); got != 0 {
		t.Errorf("expected 0 calls for unsubscribed event, got %d", got)
	}

	if err := provider.Send(cfg, Payload{Event: SyncTest}); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if got := len(transport.requests); got != 1 {
		t.Errorf("expected 1 call after sync.test, got %d", got)
	}

	var body struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(transport.bodies[0], &body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if body.Text != "wireops test notification" {
		t.Errorf("text = %q, want wireops test notification", body.Text)
	}
}

func TestMaskSecret(t *testing.T) {
	if MaskSecret("") != "" {
		t.Error("empty secret should return empty string")
	}
	if MaskSecret("mysecret") != "••••••••" {
		t.Error("non-empty secret should be masked")
	}
}
