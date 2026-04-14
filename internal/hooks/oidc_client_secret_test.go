package hooks

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

func TestClearPersistedOIDCClientSecret(t *testing.T) {
	col := core.NewAuthCollection("sso_users")
	col.OAuth2.Enabled = true
	col.OAuth2.Providers = []core.OAuth2ProviderConfig{
		{Name: "oidc", ClientSecret: "must-not-persist"},
		{Name: "google", ClientSecret: "keep-for-other-providers"},
	}

	ClearPersistedOIDCClientSecret(col)

	if col.OAuth2.Providers[0].ClientSecret != "" {
		t.Fatalf("oidc secret: got %q, want empty", col.OAuth2.Providers[0].ClientSecret)
	}
	if col.OAuth2.Providers[1].ClientSecret != "keep-for-other-providers" {
		t.Fatalf("google secret was cleared unexpectedly")
	}
}

func TestClearPersistedOIDCClientSecretNilCollection(t *testing.T) {
	ClearPersistedOIDCClientSecret(nil)
}
