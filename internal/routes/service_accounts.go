package routes

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/audit"
	wireauth "github.com/wireops/wireops/internal/auth"
	"github.com/wireops/wireops/internal/rbac"
)

func RegisterServiceAccountRoutes(r *router.Router[*core.RequestEvent], app core.App) {
	r.GET("/api/custom/service-accounts", func(e *core.RequestEvent) error {
		accounts, err := app.FindAllRecords("service_accounts")
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		result := make([]map[string]any, 0, len(accounts))
		for _, account := range accounts {
			var keySummary map[string]any = nil
			if account.GetString("key_prefix") != "" {
				keySummary = map[string]any{
					"key_prefix":   account.GetString("key_prefix"),
					"expires_at":   account.GetDateTime("key_expires_at").String(),
					"last_used_at": account.GetDateTime("key_last_used_at").String(),
					"revoked":      account.GetBool("key_revoked"),
					"created":      account.GetDateTime("updated").String(),
				}
			}

			createdByID := account.GetString("created_by")
			createdByEmail := ""
			if createdByID != "" {
				if creator, err := app.FindRecordById("users", createdByID); err == nil {
					createdByEmail = creator.GetString("email")
				}
			}

			result = append(result, map[string]any{
				"id":               account.Id,
				"name":             account.GetString("name"),
				"description":      account.GetString("description"),
				"role":             account.GetString("role"),
				"enabled":          account.GetBool("enabled"),
				"created_by":       createdByID,
				"created_by_email": createdByEmail,
				"created":          account.GetDateTime("created").String(),
				"updated":          account.GetDateTime("updated").String(),
				"key":              keySummary,
			})
		}
		return e.JSON(http.StatusOK, result)
	}).BindFunc(rbac.Require(rbac.CapManageSecurity))

	r.POST("/api/custom/service-accounts", func(e *core.RequestEvent) error {
		var body struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Role        string `json:"role"`
			Enabled     *bool  `json:"enabled"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		body.Name = strings.TrimSpace(body.Name)
		body.Description = strings.TrimSpace(body.Description)
		role := rbac.NormalizeRole(body.Role)
		if body.Name == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
		}
		if body.Description == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "description is required"})
		}
		if role != rbac.RoleViewer && role != rbac.RoleOperator {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "service accounts can only be viewers or operators"})
		}
		enabled := true
		if body.Enabled != nil {
			enabled = *body.Enabled
		}

		if !enabled {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "keys cannot be generated for disabled service accounts"})
		}

		key, err := wireauth.GenerateAPIKey()
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to generate api key"})
		}

		col, err := app.FindCollectionByNameOrId("service_accounts")
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "service_accounts collection not found"})
		}
		rec := core.NewRecord(col)
		rec.Set("name", body.Name)
		rec.Set("description", body.Description)
		rec.Set("role", role)
		rec.Set("enabled", enabled)
		keyHash, err := wireauth.HashAPIKey(key)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "api key hashing is not configured"})
		}
		rec.Set("key_hash", keyHash)
		rec.Set("key_prefix", wireauth.APIKeyPrefix(key))
		rec.Set("key_revoked", false)
		rec.Set("key_last_used_at", nil)
		if e.Auth != nil && e.Auth.Collection() != nil && e.Auth.Collection().Name == "users" {
			rec.Set("created_by", e.Auth.Id)
		}
		if err := app.Save(rec); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		audit.RecordRequest(app, e, audit.Event{
			Action:       "api_key.issue",
			ResourceType: "api_key",
			ResourceID:   rec.Id,
			Metadata: map[string]any{
				"service_account": rec.Id,
				"key_prefix":      rec.GetString("key_prefix"),
			},
		})
		return e.JSON(http.StatusCreated, map[string]any{
			"id":          rec.Id,
			"name":        rec.GetString("name"),
			"description": rec.GetString("description"),
			"role":        rec.GetString("role"),
			"enabled":     rec.GetBool("enabled"),
			"api_key":     key,
			"key_prefix":  rec.GetString("key_prefix"),
		})
	}).BindFunc(rbac.Require(rbac.CapManageSecurity))

	r.PUT("/api/custom/service-accounts/{id}", func(e *core.RequestEvent) error {
		rec, err := app.FindRecordById("service_accounts", e.Request.PathValue("id"))
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "service account not found"})
		}
		var body struct {
			Name        *string `json:"name"`
			Description *string `json:"description"`
			Role        *string `json:"role"`
			Enabled     *bool   `json:"enabled"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		if body.Name != nil {
			trimmed := strings.TrimSpace(*body.Name)
			if trimmed == "" {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
			}
			rec.Set("name", trimmed)
		}
		if body.Description != nil {
			trimmed := strings.TrimSpace(*body.Description)
			if trimmed == "" {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "description is required"})
			}
			rec.Set("description", trimmed)
		}
		if body.Role != nil {
			role := rbac.NormalizeRole(*body.Role)
			if role != rbac.RoleViewer && role != rbac.RoleOperator {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "service accounts can only be viewers or operators"})
			}
			rec.Set("role", role)
		}
		if body.Enabled != nil {
			if *body.Enabled && !rec.GetBool("enabled") {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "disabled service accounts cannot be re-enabled"})
			}
			rec.Set("enabled", *body.Enabled)
			if !*body.Enabled {
				rec.Set("key_hash", "")
				rec.Set("key_prefix", "")
				rec.Set("key_revoked", true)
				rec.Set("key_expires_at", nil)
				rec.Set("key_last_used_at", nil)
			}
		}
		if err := app.Save(rec); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "saved"})
	}).BindFunc(rbac.Require(rbac.CapManageSecurity))

	r.DELETE("/api/custom/service-accounts/{id}", func(e *core.RequestEvent) error {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": "deleting service accounts is not allowed to preserve audit logs; please disable the account instead"})
	}).BindFunc(rbac.Require(rbac.CapManageSecurity))

	r.POST("/api/custom/service-accounts/{id}/keys", func(e *core.RequestEvent) error {
		account, err := app.FindRecordById("service_accounts", e.Request.PathValue("id"))
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "service account not found"})
		}
		var body struct {
			ExpiresAt *string `json:"expires_at"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil && err.Error() != "EOF" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}

		key, err := wireauth.GenerateAPIKey()
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to generate api key"})
		}

		keyHash, err := wireauth.HashAPIKey(key)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "api key hashing is not configured"})
		}
		account.Set("key_hash", keyHash)
		account.Set("key_prefix", wireauth.APIKeyPrefix(key))
		account.Set("key_revoked", false)
		account.Set("key_last_used_at", nil)
		if body.ExpiresAt != nil && strings.TrimSpace(*body.ExpiresAt) != "" {
			parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*body.ExpiresAt))
			if err != nil {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "expires_at must be RFC3339"})
			}
			account.Set("key_expires_at", parsed)
		} else {
			account.Set("key_expires_at", nil)
		}
		if err := app.Save(account); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		audit.RecordRequest(app, e, audit.Event{
			Action:       "api_key.issue",
			ResourceType: "api_key",
			ResourceID:   account.Id,
			Metadata: map[string]any{
				"service_account": account.Id,
				"key_prefix":      account.GetString("key_prefix"),
			},
		})
		return e.JSON(http.StatusCreated, map[string]any{
			"id":         account.Id,
			"api_key":    key,
			"key_prefix": account.GetString("key_prefix"),
		})
	}).BindFunc(rbac.Require(rbac.CapManageSecurity))

	r.DELETE("/api/custom/service-accounts/{id}/keys", func(e *core.RequestEvent) error {
		account, err := app.FindRecordById("service_accounts", e.Request.PathValue("id"))
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "service account not found"})
		}
		account.Set("key_revoked", true)
		if err := app.Save(account); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		audit.RecordRequest(app, e, audit.Event{
			Action:       "api_key.revoke",
			ResourceType: "api_key",
			ResourceID:   account.Id,
			Metadata: map[string]any{
				"service_account": account.Id,
				"key_prefix":      account.GetString("key_prefix"),
			},
		})
		return e.JSON(http.StatusOK, map[string]string{"status": "revoked"})
	}).BindFunc(rbac.Require(rbac.CapManageSecurity))
}
