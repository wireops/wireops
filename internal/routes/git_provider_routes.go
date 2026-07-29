package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/git"
	"github.com/wireops/wireops/internal/gitprovider"
	"github.com/wireops/wireops/internal/rbac"
)

// registerGitProviderRoutes wires up native git-hosting OAuth: connecting
// an account, and browsing its orgs/repos/branches to pick a repository
// instead of hand-typing a git_url. Provider-agnostic: every route is
// parameterized by {slug} and dispatches through the internal/gitprovider
// registry, so adding Gitea/Forgejo/GitLab later needs no new routes here.
func (rr routeRegistrar) registerGitProviderRoutes() {
	rr.r.GET("/api/custom/git-providers", func(e *core.RequestEvent) error {
		type providerOut struct {
			Slug         string `json:"slug"`
			Name         string `json:"name"`
			Configured   bool   `json:"configured"`
			Connected    bool   `json:"connected"`
			AccountLogin string `json:"account_login,omitempty"`
			KeyID        string `json:"key_id,omitempty"`
		}
		var out []providerOut
		for _, p := range gitprovider.All() {
			item := providerOut{Slug: p.Slug(), Name: p.Name(), Configured: p.Configured()}
			if key, err := findOAuthRepositoryKey(rr.app, p.Slug()); err == nil {
				item.Connected = true
				item.KeyID = key.Id
				item.AccountLogin = key.GetString("oauth_account_login")
			}
			out = append(out, item)
		}
		if out == nil {
			out = []providerOut{}
		}
		return e.JSON(http.StatusOK, out)
	}).BindFunc(rbac.Require(rbac.CapManageRepos))

	rr.r.POST("/api/custom/git-providers/{slug}/authorize-url", func(e *core.RequestEvent) error {
		slug := e.Request.PathValue("slug")
		provider, ok := gitprovider.Get(slug)
		if !ok || !provider.Configured() {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "unknown or unconfigured git provider"})
		}

		var userID string
		if e.Auth != nil {
			userID = e.Auth.Id
		}
		state, err := gitprovider.SignState(userID)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		url := provider.AuthorizeURL(state, config.GetGitProviderCallbackURL(slug))
		return e.JSON(http.StatusOK, map[string]string{"url": url})
	}).BindFunc(rbac.Require(rbac.CapManageRepos))

	// Public: self-authenticates via the signed state token (no session
	// cookie/API key is available on the OAuth provider's redirect back).
	rr.r.GET("/api/custom/git-providers/{slug}/callback", func(e *core.RequestEvent) error {
		slug := e.Request.PathValue("slug")
		provider, ok := gitprovider.Get(slug)
		if !ok || !provider.Configured() {
			return oauthCallbackResult(e, false, slug, "", "", "unknown or unconfigured git provider")
		}

		state := e.Request.URL.Query().Get("state")
		if _, err := gitprovider.VerifyState(state); err != nil {
			return oauthCallbackResult(e, false, slug, "", "", "invalid or expired state")
		}

		code := e.Request.URL.Query().Get("code")
		if code == "" {
			return oauthCallbackResult(e, false, slug, "", "", "missing code")
		}

		token, err := provider.ExchangeCode(e.Request.Context(), code, config.GetGitProviderCallbackURL(slug))
		if err != nil {
			return oauthCallbackResult(e, false, slug, "", "", err.Error())
		}

		keyID, err := rr.upsertOAuthRepositoryKey(provider, token)
		if err != nil {
			return oauthCallbackResult(e, false, slug, "", "", err.Error())
		}

		return oauthCallbackResult(e, true, slug, keyID, token.AccountLogin, "")
	})

	rr.r.GET("/api/custom/git-providers/{slug}/orgs", func(e *core.RequestEvent) error {
		provider, token, errResp := rr.resolveGitProviderToken(e)
		if errResp != nil {
			return errResp
		}
		orgs, err := provider.ListOrganizations(e.Request.Context(), token)
		if err != nil {
			return e.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		if orgs == nil {
			orgs = []gitprovider.Org{}
		}
		return e.JSON(http.StatusOK, orgs)
	}).BindFunc(rbac.Require(rbac.CapManageRepos))

	rr.r.GET("/api/custom/git-providers/{slug}/repos", func(e *core.RequestEvent) error {
		provider, token, errResp := rr.resolveGitProviderToken(e)
		if errResp != nil {
			return errResp
		}
		org := e.Request.URL.Query().Get("org")
		repos, err := provider.ListRepositories(e.Request.Context(), token, org)
		if err != nil {
			return e.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		if repos == nil {
			repos = []gitprovider.Repo{}
		}
		return e.JSON(http.StatusOK, repos)
	}).BindFunc(rbac.Require(rbac.CapManageRepos))

	rr.r.GET("/api/custom/git-providers/{slug}/branches", func(e *core.RequestEvent) error {
		provider, token, errResp := rr.resolveGitProviderToken(e)
		if errResp != nil {
			return errResp
		}
		repo := e.Request.URL.Query().Get("repo")
		if repo == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing repo parameter"})
		}
		branches, err := provider.ListBranches(e.Request.Context(), token, repo)
		if err != nil {
			return e.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		if branches == nil {
			branches = []gitprovider.Branch{}
		}
		return e.JSON(http.StatusOK, branches)
	}).BindFunc(rbac.Require(rbac.CapManageRepos))
}

// resolveGitProviderToken loads the {slug} provider and the OAuth token for
// the ?key= repository_keys record, writing an error response and returning
// it (non-nil) if either step fails.
func (rr routeRegistrar) resolveGitProviderToken(e *core.RequestEvent) (gitprovider.Provider, string, error) {
	slug := e.Request.PathValue("slug")
	provider, ok := gitprovider.Get(slug)
	if !ok {
		return nil, "", e.JSON(http.StatusNotFound, map[string]string{"error": "unknown git provider"})
	}

	keyID := e.Request.URL.Query().Get("key")
	if keyID == "" {
		return nil, "", e.JSON(http.StatusBadRequest, map[string]string{"error": "missing key parameter"})
	}

	tokenProviderSlug, token, err := git.LoadOAuthToken(rr.app, keyID)
	if err != nil {
		return nil, "", e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if tokenProviderSlug != slug {
		return nil, "", e.JSON(http.StatusBadRequest, map[string]string{"error": "key does not belong to this provider"})
	}

	return provider, token, nil
}

// findOAuthRepositoryKey looks up the single global repository_keys row
// holding this provider's connected OAuth token, if any. Shared by the
// upsert-on-connect path and by anything that needs to know/display
// connection status (providers list, integration test route) without
// duplicating the filter.
func findOAuthRepositoryKey(app core.App, slug string) (*core.Record, error) {
	return app.FindFirstRecordByFilter(
		"repository_keys",
		"auth_type = {:auth_type} && oauth_provider = {:provider}",
		map[string]any{"auth_type": "oauth_token", "provider": slug},
	)
}

// upsertOAuthRepositoryKey saves the connected account's OAuth token as a
// single, global repository_keys record per provider — connecting again
// (reconnect, refreshed token, different account) updates that same row in
// place rather than creating a duplicate, so every repository picked
// through this provider's picker shares one reusable credential (migration
// 38 already made repository_keys many-to-one; this just stops the OAuth
// path from bypassing that by always inserting).
func (rr routeRegistrar) upsertOAuthRepositoryKey(provider gitprovider.Provider, token *gitprovider.Token) (string, error) {
	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
	if len(secretKey) != 32 {
		return "", fmt.Errorf("SECRET_KEY must be exactly 32 bytes")
	}

	encryptedToken, err := crypto.Encrypt([]byte(token.AccessToken), secretKey)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt oauth token: %w", err)
	}

	var encryptedRefresh string
	if token.RefreshToken != "" {
		encryptedRefresh, err = crypto.Encrypt([]byte(token.RefreshToken), secretKey)
		if err != nil {
			return "", fmt.Errorf("failed to encrypt oauth refresh token: %w", err)
		}
	}

	var recID string
	txErr := rr.app.RunInTransaction(func(txApp core.App) error {
		var rec *core.Record
		if existing, err := findOAuthRepositoryKey(txApp, provider.Slug()); err == nil {
			rec = existing
		} else {
			col, err := txApp.FindCollectionByNameOrId("repository_keys")
			if err != nil {
				return err
			}
			rec = core.NewRecord(col)
		}
		rec.Set("name", fmt.Sprintf("%s (@%s)", provider.Name(), token.AccountLogin))
		rec.Set("auth_type", "oauth_token")
		rec.Set("oauth_provider", provider.Slug())
		rec.Set("oauth_token", encryptedToken)
		rec.Set("oauth_account_login", token.AccountLogin)
		if encryptedRefresh != "" {
			rec.Set("oauth_refresh_token", encryptedRefresh)
		}
		if !token.ExpiresAt.IsZero() {
			rec.Set("oauth_token_expires_at", token.ExpiresAt)
		}

		if err := txApp.Save(rec); err != nil {
			return fmt.Errorf("failed to save repository key: %w", err)
		}
		recID = rec.Id
		return nil
	})
	if txErr != nil {
		return "", txErr
	}
	return recID, nil
}

// oauthCallbackResult renders the tiny HTML page the OAuth popup loads at
// the end of the flow: it posts the result back to the opener window and
// closes itself, so RepositoryCreateModal's in-progress form state survives
// the round trip instead of being lost to a full-page redirect.
func oauthCallbackResult(e *core.RequestEvent, ok bool, provider, keyID, login, errMsg string) error {
	payload, _ := json.Marshal(map[string]any{
		"type":     "wireops-git-oauth",
		"ok":       ok,
		"provider": provider,
		"keyId":    keyID,
		"login":    login,
		"error":    errMsg,
	})

	e.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	e.Response.WriteHeader(http.StatusOK)
	// targetOrigin is "*": this callback page's own origin is the backend's
	// (e.g. localhost:8090 in dev), which is not necessarily the opener's
	// origin (e.g. localhost:3000 for the Nuxt dev server) — postMessage
	// would silently fail to deliver if the target origin is pinned wrong.
	// The payload carries no secrets (a repository_keys record id/login),
	// and the frontend verifies ev.source === the exact popup it opened.
	_, err := fmt.Fprintf(e.Response, `<!DOCTYPE html><html><body><script>
if (window.opener) { window.opener.postMessage(%s, '*'); }
window.close();
</script></body></html>`, payload)
	return err
}
