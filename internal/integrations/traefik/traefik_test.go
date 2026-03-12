package traefik

import (
	"testing"

	"github.com/wireops/wireops/internal/integrations"
)

func TestTraefikResolveContainerActions(t *testing.T) {
	integration := &TraefikIntegration{}

	ctx := integrations.ContainerContext{
		ContainerID:   "123",
		ContainerName: "test-app",
		Labels: map[string]string{
			"traefik.enable":                      "true",
			"traefik.http.routers.test-app.rule": "Host(`sub.example.com`)",
		},
	}

	actions := integration.ResolveContainerActions(nil, ctx)
	if len(actions) == 0 {
		t.Fatalf("expected 1 action, got 0")
	}

	action := actions[0]
	if action.URL != "https://sub.example.com" {
		t.Errorf("expected URL https://sub.example.com, got %s", action.URL)
	}
	if action.Kind != integrations.ActionKindReverseProxy {
		t.Errorf("expected kind reverse-proxy, got %v", action.Kind)
	}

	// Test with custom scheme
	cfg := map[string]interface{}{
		"scheme": "http",
		"port":   "8080",
	}

	actions = integration.ResolveContainerActions(cfg, ctx)
	if len(actions) == 0 {
		t.Fatalf("expected 1 action with config, got 0")
	}
	action = actions[0]
	if action.URL != "http://sub.example.com:8080" {
		t.Errorf("expected URL http://sub.example.com:8080, got %s", action.URL)
	}
}
