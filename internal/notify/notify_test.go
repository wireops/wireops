package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestWebhookProvider verifies the webhook implementation.
func TestWebhookProvider_Send(t *testing.T) {
	var received struct {
		method  string
		body    []byte
		headers http.Header
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.method = r.Method
		received.headers = r.Header.Clone()
		received.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := &WebhookProvider{
		client: &http.Client{Timeout: 5 * time.Second},
	}

	cfg := &Config{
		Provider: "webhook",
		URL:      server.URL,
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
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := &WebhookProvider{client: &http.Client{}}
	cfg := &Config{
		URL:     server.URL,
		Events:  []string{SyncDone},
		Enabled: true,
	}

	// Unsubscribed event -> no call
	if err := provider.Send(cfg, Payload{Event: SyncError}); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if callCount != 0 {
		t.Errorf("expected 0 calls for unsubscribed event, got %d", callCount)
	}

	// Subscribed event -> call
	if err := provider.Send(cfg, Payload{Event: SyncDone}); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call for subscribed event, got %d", callCount)
	}

	// Test event (not in list) -> call (bypass)
	if err := provider.Send(cfg, Payload{Event: SyncTest}); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls after sync.test, got %d", callCount)
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.path = r.URL.Path
		received.headers = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		received.body = string(b)
		received.auth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := &NtfyProvider{
		client: &http.Client{},
	}

	cfg := &Config{
		Provider:  "ntfy",
		URL:       server.URL,
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
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := &NtfyProvider{client: &http.Client{}}
	cfg := &Config{
		URL:          server.URL,
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

	expected := "Hello world, status is sync.done"
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
