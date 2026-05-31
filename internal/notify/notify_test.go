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

// TestWebhookProvider verifies the webhook implementation.
func TestWebhookProvider_Send(t *testing.T) {
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

// TestWebhookProvider_EventFiltering verifies subscription logic + test event bypass.
func TestWebhookProvider_EventFiltering(t *testing.T) {
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

// TestNtfyProvider verifies ntfy integration.
func TestNtfyProvider_Send(t *testing.T) {
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

// TestNtfyProvider_Template verifies custom template rendering.
func TestNtfyProvider_Template(t *testing.T) {
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

func TestMaskSecret(t *testing.T) {
	if MaskSecret("") != "" {
		t.Error("empty secret should return empty string")
	}
	if MaskSecret("mysecret") != "••••••••" {
		t.Error("non-empty secret should be masked")
	}
}
