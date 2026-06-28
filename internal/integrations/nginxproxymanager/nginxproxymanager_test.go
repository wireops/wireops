package nginxproxymanager

import (
	"testing"

	"github.com/wireops/wireops/internal/integrations"
)

func TestNginxProxyManagerResolveContainerActions(t *testing.T) {
	integration := &NginxProxyManagerIntegration{}
	ctx := integrations.ContainerContext{
		ContainerID:   "123",
		ContainerName: "test-app",
		Labels: map[string]string{
			"dev.wireops.npm.host": "app.example.com",
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

func TestNginxProxyManagerResolveContainerActionsWithGenericHints(t *testing.T) {
	integration := &NginxProxyManagerIntegration{}
	ctx := integrations.ContainerContext{
		Labels: map[string]string{
			"dev.wireops.proxy.hosts":  "app.example.com, https://api.example.com",
			"dev.wireops.proxy.scheme": "http",
		},
	}

	actions := integration.ResolveContainerActions(map[string]interface{}{"port": "8080"}, ctx)
	if len(actions) != 2 {
		t.Fatalf("actions = %d, want 2", len(actions))
	}
	want := []string{
		"http://api.example.com:8080",
		"http://app.example.com:8080",
	}
	for i, expected := range want {
		if actions[i].URL != expected {
			t.Errorf("actions[%d].URL = %q, want %q", i, actions[i].URL, expected)
		}
	}
}

func TestNginxProxyManagerResolveContainerActionsAggregatesSingularAndPluralHints(t *testing.T) {
	integration := &NginxProxyManagerIntegration{}
	ctx := integrations.ContainerContext{
		Labels: map[string]string{
			"dev.wireops.npm.host":     "app.example.com",
			"dev.wireops.npm.hosts":    "api.example.com app.example.com",
			"dev.wireops.proxy.host":   "admin.example.com",
			"dev.wireops.proxy.hosts":  "docs.example.com",
			"dev.wireops.proxy.scheme": "http",
		},
	}

	actions := integration.ResolveContainerActions(nil, ctx)
	if len(actions) != 4 {
		t.Fatalf("actions = %d, want 4", len(actions))
	}
	want := []string{
		"http://admin.example.com",
		"http://api.example.com",
		"http://app.example.com",
		"http://docs.example.com",
	}
	for i, expected := range want {
		if actions[i].URL != expected {
			t.Errorf("actions[%d].URL = %q, want %q", i, actions[i].URL, expected)
		}
	}
}

func TestNginxProxyManagerResolveContainerActionsPreservesBareAddressPort(t *testing.T) {
	integration := &NginxProxyManagerIntegration{}
	ctx := integrations.ContainerContext{
		Labels: map[string]string{
			"dev.wireops.npm.host":    "app.example.com:8443",
			"dev.wireops.proxy.hosts": "https://app.example.com:8443",
		},
	}

	actions := integration.ResolveContainerActions(nil, ctx)
	if len(actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(actions))
	}
	if actions[0].URL != "https://app.example.com:8443" {
		t.Errorf("URL = %q, want https://app.example.com:8443", actions[0].URL)
	}
}

func TestNginxProxyManagerResolveContainerActionsAdminLink(t *testing.T) {
	integration := &NginxProxyManagerIntegration{}
	ctx := integrations.ContainerContext{
		Labels: map[string]string{
			"dev.wireops.npm.hosts": "app.example.com api.example.com",
		},
	}

	actions := integration.ResolveContainerActions(map[string]interface{}{
		"admin_url": "https://npm.example.com/",
	}, ctx)
	if len(actions) != 3 {
		t.Fatalf("actions = %d, want 3", len(actions))
	}
	if actions[2].Label != "NPM Admin" {
		t.Errorf("admin label = %q, want NPM Admin", actions[2].Label)
	}
	if actions[2].URL != "https://npm.example.com" {
		t.Errorf("admin URL = %q, want https://npm.example.com", actions[2].URL)
	}
}

func TestNginxProxyManagerResolveContainerActionsLocalFilter(t *testing.T) {
	integration := &NginxProxyManagerIntegration{}
	ctx := integrations.ContainerContext{
		Labels: map[string]string{
			"dev.wireops.npm.hosts": "localhost app.example.com",
		},
	}

	actions := integration.ResolveContainerActions(map[string]interface{}{"allow_local_hosts": false}, ctx)
	if len(actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(actions))
	}
	if actions[0].URL != "https://app.example.com" {
		t.Errorf("URL = %q, want https://app.example.com", actions[0].URL)
	}
}
