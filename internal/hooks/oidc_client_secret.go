package hooks

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const oidcProviderName = "oidc"

// SSOUsersOAuthRuntimeMiddleware sets the OIDC provider client secret from OIDC_CLIENT_SECRET
// on the cached sso_users collection for PocketBase OAuth2 handlers, then reloads the collection
// cache after the request so the secret is not left in memory for other routes.
func SSOUsersOAuthRuntimeMiddleware(app core.App) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if !ssoOAuthRequestNeedsRuntimeSecret(e.Request.Method, e.Request.URL.Path) {
			return e.Next()
		}

		secret := strings.TrimSpace(os.Getenv("OIDC_CLIENT_SECRET"))
		if err := injectOIDCClientSecretInCache(app, secret); err != nil {
			log.Printf("[oidc] inject client secret for OAuth request: %v", err)
			return e.Next()
		}

		defer func() {
			if err := app.ReloadCachedCollections(); err != nil {
				log.Printf("[oidc] reload collection cache after OAuth request: %v", err)
			}
		}()

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

func injectOIDCClientSecretInCache(app core.App, secret string) error {
	col, err := app.FindCachedCollectionByNameOrId("sso_users")
	if err != nil {
		return err
	}
	if !col.OAuth2.Enabled {
		return nil
	}
	for i := range col.OAuth2.Providers {
		if col.OAuth2.Providers[i].Name == oidcProviderName {
			col.OAuth2.Providers[i].ClientSecret = secret
			break
		}
	}
	return nil
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
