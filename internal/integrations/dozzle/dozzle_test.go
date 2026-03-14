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
	var config map[string]interface{} // config is nil for this test case
	actions := integration.ResolveContainerActions(config, ctx)
	if len(actions) != 0 {
		t.Fatalf("expected 0 actions when URL is missing, got %d", len(actions))
	}

	// Test with URL config
	config = map[string]interface{}{
		"url": "http://dozzle.local:8080/", // Check trailing slash removal
	}

	actions = integration.ResolveContainerActions(config, ctx)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}

	action := actions[0]
	if action.Kind != integrations.ActionKindLog {
		t.Errorf("expected kind log, got %v", action.Kind)
	}

	expectedURL := "http://dozzle.local:8080/container/1234"
	if action.URL != expectedURL {
		t.Errorf("expected URL %s, got %s", expectedURL, action.URL)
	}
}
