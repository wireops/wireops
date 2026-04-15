package hooks

import (
	"net/http"
	"os"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const oidcProviderName = "oidc"

// SSOUsersOAuthRuntimeMiddleware injects the OIDC provider client secret from OIDC_CLIENT_SECRET
// into the sso_users collection for PocketBase OAuth2 handlers.
//
// Instead of mutating the shared global collection cache, it replaces e.App with a
// per-request proxy whose FindCachedCollectionByNameOrId returns a shallow copy of
// the sso_users collection with a fresh Providers slice containing the injected secret.
// The shared cached pointer is never written to, eliminating any race window.
func SSOUsersOAuthRuntimeMiddleware(app core.App) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if !ssoOAuthRequestNeedsRuntimeSecret(e.Request.Method, e.Request.URL.Path) {
			return e.Next()
		}

		secret := strings.TrimSpace(os.Getenv("OIDC_CLIENT_SECRET"))
		e.App = &oidcSecretApp{App: app, secret: secret}

		return e.Next()
	}
}

func ssoOAuthRequestNeedsRuntimeSecret(method, path string) bool {
	if !strings.Contains(path, "/api/collections/") {
		return false
	}
	if strings.HasSuffix(path, "/auth-methods") && method == http.MethodGet {
		return true
	}
	if strings.HasSuffix(path, "/auth-with-oauth2") && method == http.MethodPost {
		return true
	}
	return false
}

// oidcSecretApp is a per-request core.App proxy that shadows
// FindCachedCollectionByNameOrId to return a request-local copy of the
// sso_users collection with the OIDC client secret injected, so the
// shared cache is never mutated.
type oidcSecretApp struct {
	core.App
	secret string
}

// FindCachedCollectionByNameOrId delegates to the underlying App for every
// collection except sso_users. For sso_users it returns a shallow copy whose
// Providers slice has the OIDC client secret set, leaving the shared cached
// pointer untouched.
func (a *oidcSecretApp) FindCachedCollectionByNameOrId(nameOrId string) (*core.Collection, error) {
	col, err := a.App.FindCachedCollectionByNameOrId(nameOrId)
	if err != nil || col == nil || col.Name != "sso_users" || !col.OAuth2.Enabled {
		return col, err
	}

	// Shallow-copy the Collection value and replace only the Providers slice so
	// the shared cached pointer is never written to.
	colCopy := *col
	providers := make([]core.OAuth2ProviderConfig, len(col.OAuth2.Providers))
	copy(providers, col.OAuth2.Providers)
	for i := range providers {
		if providers[i].Name == oidcProviderName {
			providers[i].ClientSecret = a.secret
			break
		}
	}
	colCopy.OAuth2.Providers = providers
	return &colCopy, nil
}

// ClearPersistedOIDCClientSecret removes the wireops-managed OIDC client secret from the
// collection model so it is never written to the database (source of truth is OIDC_CLIENT_SECRET).
func ClearPersistedOIDCClientSecret(col *core.Collection) {
	if col == nil || col.Name != "sso_users" {
		return
	}
	for i := range col.OAuth2.Providers {
		if col.OAuth2.Providers[i].Name == oidcProviderName {
			col.OAuth2.Providers[i].ClientSecret = ""
		}
	}
}
