package routes

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
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
			keys, _ := app.FindAllRecords("api_keys", dbx.HashExp{"service_account": account.Id})
			keySummaries := make([]map[string]any, 0, len(keys))
			for _, key := range keys {
				keySummaries = append(keySummaries, map[string]any{
					"id":           key.Id,
					"name":         key.GetString("name"),
					"key_prefix":   key.GetString("key_prefix"),
					"expires_at":   key.GetDateTime("expires_at").String(),
					"last_used_at": key.GetDateTime("last_used_at").String(),
					"revoked":      key.GetBool("revoked"),
					"created":      key.GetDateTime("created").String(),
				})
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
				"keys":             keySummaries,
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
		role := rbac.NormalizeRole(body.Role)
		if body.Name == "" || role == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "name and valid role are required"})
		}
		enabled := true
		if body.Enabled != nil {
			enabled = *body.Enabled
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
		if e.Auth != nil && e.Auth.Collection() != nil && e.Auth.Collection().Name == "users" {
			rec.Set("created_by", e.Auth.Id)
		}
		if err := app.Save(rec); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusCreated, map[string]any{
			"id":          rec.Id,
			"name":        rec.GetString("name"),
			"description": rec.GetString("description"),
			"role":        rec.GetString("role"),
			"enabled":     rec.GetBool("enabled"),
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
			rec.Set("name", strings.TrimSpace(*body.Name))
		}
		if body.Description != nil {
			rec.Set("description", *body.Description)
		}
		if body.Role != nil {
			role := rbac.NormalizeRole(*body.Role)
			if role == "" {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid role"})
			}
			rec.Set("role", role)
		}
		if body.Enabled != nil {
			rec.Set("enabled", *body.Enabled)
		}
		if err := app.Save(rec); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "saved"})
	}).BindFunc(rbac.Require(rbac.CapManageSecurity))

	r.DELETE("/api/custom/service-accounts/{id}", func(e *core.RequestEvent) error {
		rec, err := app.FindRecordById("service_accounts", e.Request.PathValue("id"))
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "service account not found"})
		}
		if err := app.Delete(rec); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "deleted"})
	}).BindFunc(rbac.Require(rbac.CapManageSecurity))

	r.POST("/api/custom/service-accounts/{id}/keys", func(e *core.RequestEvent) error {
		account, err := app.FindRecordById("service_accounts", e.Request.PathValue("id"))
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "service account not found"})
		}
		var body struct {
			Name      string  `json:"name"`
			ExpiresAt *string `json:"expires_at"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
		}

		key, err := wireauth.GenerateAPIKey()
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to generate api key"})
		}
		col, err := app.FindCollectionByNameOrId("api_keys")
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "api_keys collection not found"})
		}
		rec := core.NewRecord(col)
		rec.Set("service_account", account.Id)
		rec.Set("name", body.Name)
		rec.Set("key_hash", wireauth.HashAPIKey(key))
		rec.Set("key_prefix", wireauth.APIKeyPrefix(key))
		rec.Set("revoked", false)
		if e.Auth != nil {
			rec.Set("created_by", e.Auth.Id)
		}
		if body.ExpiresAt != nil && strings.TrimSpace(*body.ExpiresAt) != "" {
			parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*body.ExpiresAt))
			if err != nil {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "expires_at must be RFC3339"})
			}
			rec.Set("expires_at", parsed)
		}
		if err := app.Save(rec); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		audit.RecordRequest(app, e, audit.Event{
			Action:       "api_key.issue",
			ResourceType: "api_key",
			ResourceID:   rec.Id,
			Metadata: map[string]any{
				"service_account": account.Id,
				"key_prefix":      rec.GetString("key_prefix"),
			},
		})
		return e.JSON(http.StatusCreated, map[string]any{
			"id":         rec.Id,
			"api_key":    key,
			"key_prefix": rec.GetString("key_prefix"),
		})
	}).BindFunc(rbac.Require(rbac.CapManageSecurity))

	r.DELETE("/api/custom/service-accounts/{id}/keys/{keyId}", func(e *core.RequestEvent) error {
		rec, err := app.FindRecordById("api_keys", e.Request.PathValue("keyId"))
		if err != nil || rec.GetString("service_account") != e.Request.PathValue("id") {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "api key not found"})
		}
		rec.Set("revoked", true)
		if err := app.Save(rec); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		audit.RecordRequest(app, e, audit.Event{
			Action:       "api_key.revoke",
			ResourceType: "api_key",
			ResourceID:   rec.Id,
			Metadata: map[string]any{
				"service_account": e.Request.PathValue("id"),
				"key_prefix":      rec.GetString("key_prefix"),
			},
		})
		return e.JSON(http.StatusOK, map[string]string{"status": "revoked"})
	}).BindFunc(rbac.Require(rbac.CapManageSecurity))
}

