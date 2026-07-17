package routes

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/audit"
	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/safepath"
	"github.com/wireops/wireops/internal/secrets"
)

// registerSopsRoutes exposes read-only visibility into a stack's
// SOPS-managed env vars: key names only, never values, so the frontend can
// render disabled/immutable rows for them (P1.5) without the browser ever
// seeing a decrypted secret.
func (rr routeRegistrar) registerSopsRoutes() {
	rr.r.GET("/api/custom/stacks/{id}/sops-env-vars", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		stack, err := rr.app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}

		// SOPS follows the GitOps flow only: local-only stacks have no
		// repository record and no wireops.yaml, so there's nowhere for a
		// secrets.yaml to live.
		if stack.GetString("source_type") == "local" {
			return e.JSON(http.StatusOK, map[string]any{"keys": []string{}, "available": false})
		}

		repoID := stack.GetString("repository")
		repo, err := rr.app.FindRecordById("repositories", repoID)
		if err != nil {
			return e.JSON(http.StatusOK, map[string]any{"keys": []string{}, "available": false})
		}

		composePath := stack.GetString("compose_path")
		if err := safepath.ValidateComposePath(composePath); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		workDir := filepath.Join(config.GetReposWorkspace(), repoID)
		if composePath != "" && composePath != "." {
			workDir = filepath.Join(workDir, composePath)
		}

		path, err := secrets.FindSecretsFile(workDir)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		if path == "" {
			return e.JSON(http.StatusOK, map[string]any{"keys": []string{}, "available": false})
		}

		encryptedKey := repo.GetString("sops_age_key")
		if encryptedKey == "" {
			return e.JSON(http.StatusOK, map[string]any{
				"keys":        []string{},
				"available":   true,
				"source_file": filepath.Base(path),
				"error":       "repository has no SOPS age key configured",
			})
		}

		secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
		ageKey, err := crypto.Decrypt(encryptedKey, secretKey)
		if err != nil {
			return e.JSON(http.StatusOK, map[string]any{
				"keys":        []string{},
				"available":   true,
				"source_file": filepath.Base(path),
				"error":       "failed to decrypt repository SOPS age key",
			})
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return e.JSON(http.StatusOK, map[string]any{
				"keys":        []string{},
				"available":   true,
				"source_file": filepath.Base(path),
				"error":       "failed to read secrets file",
			})
		}

		values, err := secrets.DecryptSecretsFile(e.Request.Context(), content, string(ageKey))
		if err != nil {
			return e.JSON(http.StatusOK, map[string]any{
				"keys":        []string{},
				"available":   true,
				"source_file": filepath.Base(path),
				"error":       err.Error(),
			})
		}

		keys := make([]string, 0, len(values))
		for k := range values {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		return e.JSON(http.StatusOK, map[string]any{
			"keys":        keys,
			"available":   true,
			"source_file": filepath.Base(path),
		})
	}).BindFunc(rbac.Require(rbac.CapViewStacks))

	// Explicit, destructive-ish action: rotating a repository's age key
	// makes every secrets.yaml previously encrypted for the old public key
	// undecryptable until re-encrypted (`sops updatekeys` / re-encrypt with
	// the new public key). Never done implicitly.
	rr.r.POST("/api/custom/repositories/{id}/sops-rotate-key", func(e *core.RequestEvent) error {
		repoID := e.Request.PathValue("id")
		repo, err := rr.app.FindRecordById("repositories", repoID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "repository not found"})
		}

		privateKey, publicKey, err := secrets.GenerateAgeKeypair()
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
		encrypted, err := crypto.Encrypt([]byte(privateKey), secretKey)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		repo.Set("sops_age_key", encrypted)
		repo.Set("sops_age_public_key", publicKey)
		if err := rr.app.Save(repo); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		audit.RecordRequest(rr.app, e, audit.Event{
			Action:       "repository.sops_key_rotate",
			ResourceType: "repository",
			ResourceID:   repoID,
		})

		return e.JSON(http.StatusOK, map[string]string{"sops_age_public_key": publicKey})
	}).BindFunc(rbac.Require(rbac.CapManageRepos))

	// Encrypts a flat key/value map into a SOPS-encrypted secrets.yaml using
	// the repository's own age public key, so operators can build one from
	// the UI without the sops CLI. Pure transform: only the public key is
	// read (never the private key), and nothing is persisted — the result is
	// handed back to the browser for the operator to copy/commit themselves,
	// keeping the server out of the git write path.
	rr.r.POST("/api/custom/repositories/{id}/sops-encrypt", func(e *core.RequestEvent) error {
		repoID := e.Request.PathValue("id")
		repo, err := rr.app.FindRecordById("repositories", repoID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "repository not found"})
		}

		publicKey := repo.GetString("sops_age_public_key")
		if publicKey == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "repository has no SOPS age key configured"})
		}

		var body struct {
			Values map[string]string `json:"values"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}

		encrypted, err := secrets.EncryptSecretsMap(e.Request.Context(), body.Values, publicKey)
		if err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}

		return e.JSON(http.StatusOK, map[string]string{
			"content":  string(encrypted),
			"filename": "secrets.yaml",
		})
	}).BindFunc(rbac.Require(rbac.CapManageRepos))
}
