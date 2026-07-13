package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	infisical "github.com/infisical/go-sdk"
	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/secrets"
)

// Infisical project/environment/secret browsing.
//
// Read-only helpers so the frontend can build a
// "<project-id>/<environment>/<secret-path>#<SECRET_NAME>" reference without
// the user hand-typing it. All calls run server-side with the stored
// Universal Auth machine identity (secrets.BuildInfisicalClient, reading the
// "infisical" row of the integrations collection) — the credentials never
// reach the browser. /browse intentionally returns only secret *names*,
// never values, so this can't be used to exfiltrate secret content.
//
// Project/environment listing has no go-sdk wrapper, so /projects and
// /project make authenticated REST calls directly (same access token the SDK
// client just obtained via Universal Auth login). Whether a project-scoped
// machine identity can call the org-wide "list all workspaces" endpoint
// varies by Infisical version/instance — some versions require broader
// org-level permission than a project-scoped identity has and 403 there,
// others allow it. /projects is tried opportunistically by the frontend; if
// it fails, /project (single project by ID, always within a project-scoped
// identity's grant) is the fallback so the picker still works either way.

type infisicalEnvironmentRef struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type infisicalProjectInfo struct {
	ID           string                    `json:"id"`
	Name         string                    `json:"name"`
	Environments []infisicalEnvironmentRef `json:"environments"`
}

type infisicalBrowseEntry struct {
	Name     string `json:"name"`
	IsFolder bool   `json:"is_folder"`
}

func (rr routeRegistrar) registerInfisicalBrowseRoutes() {
	rr.r.GET("/api/custom/integrations/infisical/projects", func(e *core.RequestEvent) error {
		client, siteURL, allowedProjectID, cancel, err := secrets.BuildInfisicalClient(e.Request.Context(), rr.app)
		if err != nil {
			return e.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		defer cancel()

		var body struct {
			Workspaces []infisicalProjectInfo `json:"workspaces"`
		}
		if err := infisicalAPIGet(e.Request.Context(), siteURL, "/api/v1/workspace", client.Auth().GetAccessToken(), &body); err != nil {
			// A project-scoped machine identity commonly lacks permission for
			// the org-wide "list all workspaces" endpoint (see comment above).
			// When the backend is restricted to a single project, fall back to
			// fetching just that project directly instead of dead-ending —
			// this is the same identity/permission the caller already has,
			// since Resolve/browse only ever touch the allowed project anyway.
			if allowedProjectID == "" {
				return e.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
			}
			var single struct {
				Workspace infisicalProjectInfo `json:"workspace"`
			}
			path := "/api/v1/workspace/" + url.PathEscape(allowedProjectID)
			if err := infisicalAPIGet(e.Request.Context(), siteURL, path, client.Auth().GetAccessToken(), &single); err != nil || single.Workspace.ID == "" {
				return e.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("infisical: failed to list projects and failed to fetch restricted project %q", allowedProjectID)})
			}
			return e.JSON(http.StatusOK, []infisicalProjectInfo{single.Workspace})
		}
		if allowedProjectID != "" {
			filtered := body.Workspaces[:0]
			for _, w := range body.Workspaces {
				if w.ID == allowedProjectID {
					filtered = append(filtered, w)
				}
			}
			body.Workspaces = filtered
		}
		return e.JSON(http.StatusOK, body.Workspaces)
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))

	rr.r.GET("/api/custom/integrations/infisical/project", func(e *core.RequestEvent) error {
		projectID := e.Request.URL.Query().Get("project_id")
		if projectID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "project_id is required"})
		}

		client, siteURL, allowedProjectID, cancel, err := secrets.BuildInfisicalClient(e.Request.Context(), rr.app)
		if err != nil {
			return e.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		defer cancel()
		if allowedProjectID != "" && projectID != allowedProjectID {
			return e.JSON(http.StatusForbidden, map[string]string{"error": fmt.Sprintf("infisical: access to project %q is not permitted", projectID)})
		}

		var body struct {
			Workspace infisicalProjectInfo `json:"workspace"`
		}
		path := "/api/v1/workspace/" + url.PathEscape(projectID)
		if err := infisicalAPIGet(e.Request.Context(), siteURL, path, client.Auth().GetAccessToken(), &body); err != nil {
			return e.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		if body.Workspace.ID == "" {
			return e.JSON(http.StatusNotFound, map[string]string{"error": fmt.Sprintf("infisical: project %q not found", projectID)})
		}
		return e.JSON(http.StatusOK, body.Workspace)
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))

	rr.r.GET("/api/custom/integrations/infisical/browse", func(e *core.RequestEvent) error {
		projectID := e.Request.URL.Query().Get("project_id")
		environment := e.Request.URL.Query().Get("environment")
		secretPath := "/" + strings.Trim(e.Request.URL.Query().Get("path"), "/")
		if projectID == "" || environment == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "project_id and environment are required"})
		}

		client, _, allowedProjectID, cancel, err := secrets.BuildInfisicalClient(e.Request.Context(), rr.app)
		if err != nil {
			return e.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		defer cancel()
		if allowedProjectID != "" && projectID != allowedProjectID {
			return e.JSON(http.StatusForbidden, map[string]string{"error": fmt.Sprintf("infisical: access to project %q is not permitted", projectID)})
		}

		folders, err := client.Folders().List(infisical.ListFoldersOptions{
			ProjectID:   projectID,
			Environment: environment,
			Path:        secretPath,
		})
		if err != nil {
			return e.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("infisical: failed to list folders at %q: %v", secretPath, err)})
		}

		secretList, err := client.Secrets().List(infisical.ListSecretsOptions{
			ProjectID:   projectID,
			Environment: environment,
			SecretPath:  secretPath,
			Recursive:   false,
		})
		if err != nil {
			return e.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("infisical: failed to list secrets at %q: %v", secretPath, err)})
		}

		out := make([]infisicalBrowseEntry, 0, len(folders)+len(secretList))
		for _, f := range folders {
			out = append(out, infisicalBrowseEntry{Name: f.Name, IsFolder: true})
		}
		for _, s := range secretList {
			out = append(out, infisicalBrowseEntry{Name: s.SecretKey, IsFolder: false})
		}
		return e.JSON(http.StatusOK, out)
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))

	rr.r.POST("/api/custom/integrations/infisical/test", func(e *core.RequestEvent) error {
		var body struct {
			SiteURL          string `json:"site_url"`
			ClientID         string `json:"client_id"`
			ClientSecret     string `json:"client_secret"`
			AllowedProjectID string `json:"allowed_project_id"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}

		cfg := map[string]interface{}{"client_id": body.ClientID, "client_secret": body.ClientSecret}
		resolved, err := rr.resolveMaskedIntegrationConfig("infisical", cfg)
		if err != nil {
			return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": err.Error()})
		}
		clientSecret, _ := cfg["client_secret"].(string)
		if resolved["client_secret"] {
			secretBytes, err := crypto.Decrypt(clientSecret, crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY")))
			if err != nil {
				return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": fmt.Sprintf("failed to decrypt stored client_secret: %v", err)})
			}
			clientSecret = string(secretBytes)
		}

		siteURL := body.SiteURL
		if siteURL == "" {
			siteURL = secrets.DefaultInfisicalSiteURL
		}

		client, cancel, err := secrets.NewInfisicalClientForConfig(e.Request.Context(), siteURL, body.ClientID, clientSecret)
		if err != nil {
			return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": fmt.Sprintf("universal auth login failed: %v", err)})
		}
		defer cancel()

		if body.AllowedProjectID != "" {
			var workspaceBody struct {
				Workspace infisicalProjectInfo `json:"workspace"`
			}
			path := "/api/v1/workspace/" + url.PathEscape(body.AllowedProjectID)
			if err := infisicalAPIGet(e.Request.Context(), siteURL, path, client.Auth().GetAccessToken(), &workspaceBody); err != nil || workspaceBody.Workspace.ID == "" {
				return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": fmt.Sprintf("project %q not found or not accessible", body.AllowedProjectID)})
			}
		}

		return e.JSON(http.StatusOK, map[string]string{"success": "true"})
	}).BindFunc(rbac.Require(rbac.CapManageSettings))
}

// infisicalAPIGet issues an authenticated GET against the Infisical REST API
// for endpoints the go-sdk doesn't wrap (project/environment listing).
func infisicalAPIGet(ctx context.Context, siteURL, path, accessToken string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, infisicalAPIURL(siteURL, path), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("infisical: request to %q failed: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("infisical: %q returned status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// infisicalAPIURL joins siteURL with an API path, matching the go-sdk's own
// url.AppendAPIEndpoint normalization (siteURL never already ends in the
// target path, so this is a plain concatenation with slash de-duplication).
func infisicalAPIURL(siteURL, path string) string {
	return strings.TrimSuffix(siteURL, "/") + path
}
