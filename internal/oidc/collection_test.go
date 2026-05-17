package oidc

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

func TestHydrateClientSecretForValidationUsesEnvironmentSecret(t *testing.T) {
	t.Setenv("OIDC_CLIENT_SECRET", "env-secret")

	col := core.NewAuthCollection("sso_users")
	col.OAuth2.Enabled = true
	col.OAuth2.Providers = []core.OAuth2ProviderConfig{
		{Name: ProviderName, ClientId: "client", ClientSecret: ""},
		{Name: "google", ClientSecret: ""},
	}

	HydrateClientSecretForValidation(col)

	if col.OAuth2.Providers[0].ClientSecret != "env-secret" {
		t.Fatalf("oidc secret: got %q, want env secret", col.OAuth2.Providers[0].ClientSecret)
	}
	if col.OAuth2.Providers[1].ClientSecret != "" {
		t.Fatalf("non-oidc secret was changed unexpectedly")
	}
	if err := col.OAuth2.Providers[0].Validate(); err != nil {
		t.Fatalf("hydrated provider did not validate: %v", err)
	}
}

func TestHydrateClientSecretForValidationUsesPlaceholder(t *testing.T) {
	t.Setenv("OIDC_CLIENT_SECRET", "")

	col := core.NewAuthCollection("sso_users")
	col.OAuth2.Enabled = true
	col.OAuth2.Providers = []core.OAuth2ProviderConfig{
		{Name: ProviderName, ClientSecret: ""},
	}

	HydrateClientSecretForValidation(col)

	if col.OAuth2.Providers[0].ClientSecret != validationClientSecret {
		t.Fatalf("oidc secret: got %q, want validation placeholder", col.OAuth2.Providers[0].ClientSecret)
	}
}

func TestClearPersistedClientSecret(t *testing.T) {
	col := core.NewAuthCollection("sso_users")
	col.OAuth2.Enabled = true
	col.OAuth2.Providers = []core.OAuth2ProviderConfig{
		{Name: ProviderName, ClientSecret: "must-not-persist"},
		{Name: "google", ClientSecret: "keep-for-other-providers"},
	}

	ClearPersistedClientSecret(col)

	if col.OAuth2.Providers[0].ClientSecret != "" {
		t.Fatalf("oidc secret: got %q, want empty", col.OAuth2.Providers[0].ClientSecret)
	}
	if col.OAuth2.Providers[1].ClientSecret != "keep-for-other-providers" {
		t.Fatalf("google secret was cleared unexpectedly")
	}
}
