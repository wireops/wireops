package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/secrets"
)

// Vault mount/secret browsing.
//
// Read-only helpers so the frontend can build a mount/path/field reference
// without the user hand-typing "mount/data/path#field". All calls run
// server-side with the stored Vault token (secrets.BuildVaultClient, reading
// the "vault" row of the integrations collection) — the token never reaches
// the browser. /fields intentionally returns only field *names*, never
// values, so this can't be used to exfiltrate secret content.

type vaultMountInfo struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

func (rr routeRegistrar) registerVaultBrowseRoutes() {
	rr.r.GET("/api/custom/integrations/vault/mounts", func(e *core.RequestEvent) error {
		client, allowedMount, err := secrets.BuildVaultClient(rr.app)
		if err != nil {
			return e.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}

		mounts, err := client.Sys().ListMountsWithContext(e.Request.Context())
		if err != nil {
			return e.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("vault: failed to list mounts: %v", err)})
		}

		out := make([]vaultMountInfo, 0, len(mounts))
		for path, mount := range mounts {
			if mount.Type != "kv" {
				continue
			}
			trimmedPath := strings.TrimSuffix(path, "/")
			if allowedMount != "" && trimmedPath != allowedMount {
				continue
			}
			version := mount.Options["version"]
			if version == "" {
				version = "1"
			}
			out = append(out, vaultMountInfo{Path: trimmedPath, Version: version})
		}
		return e.JSON(http.StatusOK, out)
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))

	rr.r.GET("/api/custom/integrations/vault/browse", func(e *core.RequestEvent) error {
		mount := strings.Trim(e.Request.URL.Query().Get("mount"), "/")
		path := strings.Trim(e.Request.URL.Query().Get("path"), "/")
		version := e.Request.URL.Query().Get("version")
		if mount == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "mount is required"})
		}

		client, allowedMount, err := secrets.BuildVaultClient(rr.app)
		if err != nil {
			return e.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		if allowedMount != "" && mount != allowedMount {
			return e.JSON(http.StatusForbidden, map[string]string{"error": fmt.Sprintf("vault: access to mount %q is not permitted", mount)})
		}

		listPath := vaultListPath(mount, path, version)
		secret, err := client.Logical().ListWithContext(e.Request.Context(), listPath)
		if err != nil {
			return e.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("vault: failed to list %q: %v", listPath, err)})
		}

		type entry struct {
			Name     string `json:"name"`
			IsFolder bool   `json:"is_folder"`
		}
		out := []entry{}
		if secret != nil && secret.Data != nil {
			if keysRaw, ok := secret.Data["keys"].([]interface{}); ok {
				for _, k := range keysRaw {
					name, ok := k.(string)
					if !ok {
						continue
					}
					out = append(out, entry{
						Name:     strings.TrimSuffix(name, "/"),
						IsFolder: strings.HasSuffix(name, "/"),
					})
				}
			}
		}
		return e.JSON(http.StatusOK, out)
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))

	rr.r.GET("/api/custom/integrations/vault/fields", func(e *core.RequestEvent) error {
		mount := strings.Trim(e.Request.URL.Query().Get("mount"), "/")
		path := strings.Trim(e.Request.URL.Query().Get("path"), "/")
		version := e.Request.URL.Query().Get("version")
		if mount == "" || path == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "mount and path are required"})
		}

		client, allowedMount, err := secrets.BuildVaultClient(rr.app)
		if err != nil {
			return e.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		if allowedMount != "" && mount != allowedMount {
			return e.JSON(http.StatusForbidden, map[string]string{"error": fmt.Sprintf("vault: access to mount %q is not permitted", mount)})
		}

		readPath := vaultReadPath(mount, path, version)
		secret, err := client.Logical().ReadWithContext(e.Request.Context(), readPath)
		if err != nil {
			return e.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("vault: failed to read %q: %v", readPath, err)})
		}
		if secret == nil || secret.Data == nil {
			return e.JSON(http.StatusOK, []string{})
		}

		data := secret.Data
		if version != "1" {
			if v2Data, ok := secret.Data["data"].(map[string]interface{}); ok {
				data = v2Data
			}
		}

		fields := make([]string, 0, len(data))
		for k := range data {
			fields = append(fields, k)
		}
		return e.JSON(http.StatusOK, fields)
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))

	rr.r.POST("/api/custom/integrations/vault/test", func(e *core.RequestEvent) error {
		var body struct {
			Address      string `json:"address"`
			Token        string `json:"token"`
			AllowedMount string `json:"allowed_mount"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}

		cfg := map[string]interface{}{"address": body.Address, "token": body.Token}
		resolved, err := rr.resolveMaskedIntegrationConfig("vault", cfg)
		if err != nil {
			return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": err.Error()})
		}
		token, _ := cfg["token"].(string)
		if resolved["token"] {
			tokenBytes, err := crypto.Decrypt(token, crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY")))
			if err != nil {
				return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": fmt.Sprintf("failed to decrypt stored token: %v", err)})
			}
			token = string(tokenBytes)
		}

		client, err := secrets.NewVaultClientForConfig(body.Address, token)
		if err != nil {
			return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": err.Error()})
		}
		if _, err := client.Auth().Token().LookupSelfWithContext(e.Request.Context()); err != nil {
			return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": fmt.Sprintf("token authentication failed: %v", err)})
		}

		allowedMount := strings.Trim(body.AllowedMount, "/")
		if allowedMount != "" {
			mounts, err := client.Sys().ListMountsWithContext(e.Request.Context())
			if err != nil {
				return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": fmt.Sprintf("failed to list mounts: %v", err)})
			}
			mount, ok := mounts[allowedMount+"/"]
			if !ok || mount.Type != "kv" {
				return e.JSON(http.StatusOK, map[string]string{"success": "false", "error": fmt.Sprintf("mount %q not found or is not a KV engine", allowedMount)})
			}
		}

		return e.JSON(http.StatusOK, map[string]string{"success": "true"})
	}).BindFunc(rbac.Require(rbac.CapManageSettings))
}

// vaultReadPath builds the Logical().Read path for a leaf secret,
// distinguishing KV v2 (data/ prefix) from KV v1 (no prefix).
func vaultReadPath(mount, path, version string) string {
	if version == "1" {
		return mount + "/" + path
	}
	return mount + "/data/" + path
}

// vaultListPath builds the Logical().List path for a folder, distinguishing
// KV v2 (metadata/ prefix) from KV v1 (no prefix).
func vaultListPath(mount, path, version string) string {
	if version == "1" {
		if path == "" {
			return mount
		}
		return mount + "/" + path
	}
	if path == "" {
		return mount + "/metadata"
	}
	return mount + "/metadata/" + path
}
