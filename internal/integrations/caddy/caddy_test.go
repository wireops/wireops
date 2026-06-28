package caddy

import (
	"testing"

	"github.com/wireops/wireops/internal/integrations"
)

func TestCaddyResolveContainerActions(t *testing.T) {
	integration := &CaddyIntegration{}
	ctx := integrations.ContainerContext{
		ContainerID:   "123",
		ContainerName: "test-app",
		Labels: map[string]string{
			"caddy":               "app.example.com",
			"caddy.reverse_proxy": "{{upstreams 3000}}",
		},
	}

	actions := integration.ResolveContainerActions(nil, ctx)
	if len(actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(actions))
	}
	if actions[0].URL != "https://app.example.com" {
		t.Errorf("URL = %q, want https://app.example.com", actions[0].URL)
	}
	if actions[0].Kind != integrations.ActionKindReverseProxy {
		t.Errorf("kind = %q, want reverse-proxy", actions[0].Kind)
	}
}

func TestCaddyResolveContainerActionsMultipleSites(t *testing.T) {
	integration := &CaddyIntegration{}
	ctx := integrations.ContainerContext{
		Labels: map[string]string{
			"caddy":   "app.example.com, api.example.com",
			"caddy_0": "https://admin.example.com",
			"caddy_1": "(shared)",
		},
	}

	actions := integration.ResolveContainerActions(map[string]interface{}{"scheme": "http", "port": "8080"}, ctx)
	if len(actions) != 3 {
		t.Fatalf("actions = %d, want 3", len(actions))
	}
	want := []string{
		"http://app.example.com:8080",
		"http://api.example.com:8080",
		"http://admin.example.com:8080",
	}
	for i, expected := range want {
		if actions[i].URL != expected {
			t.Errorf("actions[%d].URL = %q, want %q", i, actions[i].URL, expected)
		}
	}
}

func TestCaddyResolveContainerActionsWildcardAndLocalFilters(t *testing.T) {
	integration := &CaddyIntegration{}
	ctx := integrations.ContainerContext{
		Labels: map[string]string{
			"caddy": "*.example.com localhost",
		},
	}

	actions := integration.ResolveContainerActions(nil, ctx)
	if len(actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(actions))
	}
	if actions[0].URL != "https://localhost" {
		t.Errorf("URL = %q, want https://localhost", actions[0].URL)
	}

	actions = integration.ResolveContainerActions(map[string]interface{}{
		"allow_wildcards":   true,
		"allow_local_hosts": false,
	}, ctx)
	if len(actions) != 1 {
		t.Fatalf("actions with filters = %d, want 1", len(actions))
	}
	if actions[0].URL != "https://*.example.com" {
		t.Errorf("URL = %q, want https://*.example.com", actions[0].URL)
	}
}
