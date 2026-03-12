package dozzle

import (
	"testing"

	"github.com/wireops/wireops/internal/integrations"
)

func TestDozzleResolveContainerActions(t *testing.T) {
	integration := &DozzleIntegration{}

	ctx := integrations.ContainerContext{
		ContainerID:   "1234",
		ContainerName: "my-web-app",
		Labels:        map[string]string{},
	}

	// Test without URL config
	actions := integration.ResolveContainerActions(nil, ctx)
	if len(actions) != 0 {
		t.Fatalf("expected 0 actions when URL is missing, got %d", len(actions))
	}

	// Test with URL config
	cfg := map[string]interface{}{
		"url": "http://dozzle.local:8080/", // Check trailing slash removal
	}

	actions = integration.ResolveContainerActions(cfg, ctx)
	if len(actions) == 0 {
		t.Fatalf("expected 1 action, got 0")
	}

	action := actions[0]
	if action.Kind != integrations.ActionKindLog {
		t.Errorf("expected kind log, got %v", action.Kind)
	}

	expectedURL := "http://dozzle.local:8080/container/my-web-app"
	if action.URL != expectedURL {
		t.Errorf("expected URL %s, got %s", expectedURL, action.URL)
	}
}
