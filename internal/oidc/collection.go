package oidc

import (
	"os"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const (
	ProviderName = "oidc"
	ssoUsers     = "sso_users"

	// PocketBase validates enabled OAuth2 providers before serializing collections,
	// but clientSecret is intentionally stripped during serialization.
	validationClientSecret = "__wireops_oidc_runtime_secret__"
)

// HydrateClientSecretForValidation sets a transient OIDC client secret so
// PocketBase can validate and save schema-only sso_users collection changes.
// Collection serialization strips this value, so it is not persisted.
func HydrateClientSecretForValidation(col *core.Collection) {
	if col == nil || col.Name != ssoUsers || !col.OAuth2.Enabled {
		return
	}

	secret := strings.TrimSpace(os.Getenv("OIDC_CLIENT_SECRET"))
	if secret == "" {
		secret = validationClientSecret
	}

	for i := range col.OAuth2.Providers {
		if col.OAuth2.Providers[i].Name == ProviderName && col.OAuth2.Providers[i].ClientSecret == "" {
			col.OAuth2.Providers[i].ClientSecret = secret
		}
	}
}

// ClearPersistedClientSecret removes the wireops-managed OIDC client secret
// from the collection model so it is never written to the database.
func ClearPersistedClientSecret(col *core.Collection) {
	if col == nil || col.Name != ssoUsers {
		return
	}
	for i := range col.OAuth2.Providers {
		if col.OAuth2.Providers[i].Name == ProviderName {
			col.OAuth2.Providers[i].ClientSecret = ""
		}
	}
}
