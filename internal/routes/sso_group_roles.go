package routes

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/rbac"
)

func RegisterSSOGroupRoleRoutes(r *router.Router[*core.RequestEvent], app core.App) {
	r.GET("/api/custom/sso-group-roles", func(e *core.RequestEvent) error {
		records, err := app.FindAllRecords("sso_group_roles")
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		result := make([]map[string]any, 0, len(records))
		for _, rec := range records {
			result = append(result, map[string]any{
				"id":      rec.Id,
				"group":   rec.GetString("group"),
				"role":    rec.GetString("role"),
				"created": rec.GetDateTime("created").String(),
				"updated": rec.GetDateTime("updated").String(),
			})
		}
		return e.JSON(http.StatusOK, result)
	}).BindFunc(rbac.Require(rbac.CapManageSecurity))

	r.POST("/api/custom/sso-group-roles", func(e *core.RequestEvent) error {
		var body struct {
			Group string `json:"group"`
			Role  string `json:"role"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		group := strings.TrimSpace(body.Group)
		role := rbac.NormalizeRole(body.Role)
		if group == "" || role == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "group and valid role are required"})
		}
		col, err := app.FindCollectionByNameOrId("sso_group_roles")
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "sso_group_roles collection not found"})
		}
		rec := core.NewRecord(col)
		rec.Set("group", group)
		rec.Set("role", role)
		if err := app.Save(rec); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusCreated, map[string]string{"id": rec.Id})
	}).BindFunc(rbac.Require(rbac.CapManageSecurity))

	r.PUT("/api/custom/sso-group-roles/{id}", func(e *core.RequestEvent) error {
		rec, err := app.FindRecordById("sso_group_roles", e.Request.PathValue("id"))
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "mapping not found"})
		}
		var body struct {
			Group *string `json:"group"`
			Role  *string `json:"role"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		if body.Group != nil {
			group := strings.TrimSpace(*body.Group)
			if group == "" {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "group is required"})
			}
			rec.Set("group", group)
		}
		if body.Role != nil {
			role := rbac.NormalizeRole(*body.Role)
			if role == "" {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid role"})
			}
			rec.Set("role", role)
		}
		if err := app.Save(rec); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "saved"})
	}).BindFunc(rbac.Require(rbac.CapManageSecurity))

	r.DELETE("/api/custom/sso-group-roles/{id}", func(e *core.RequestEvent) error {
		rec, err := app.FindRecordById("sso_group_roles", e.Request.PathValue("id"))
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "mapping not found"})
		}
		if err := app.Delete(rec); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "deleted"})
	}).BindFunc(rbac.Require(rbac.CapManageSecurity))

	r.PUT("/api/custom/settings/sso-groups-claim", func(e *core.RequestEvent) error {
		var body struct {
			Claim string `json:"claim"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		claim := strings.TrimSpace(body.Claim)
		if claim == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "claim is required"})
		}
		rec, err := firstAppSettingsRecord(app)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		rec.Set("sso_groups_claim", claim)
		if err := app.Save(rec); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, map[string]string{"status": "saved", "claim": claim})
	}).BindFunc(rbac.Require(rbac.CapManageSecurity))
}

func firstAppSettingsRecord(app core.App) (*core.Record, error) {
	records, err := app.FindAllRecords("app_settings")
	if err != nil {
		return nil, err
	}
	if len(records) > 0 {
		return records[0], nil
	}
	col, err := app.FindCollectionByNameOrId("app_settings")
	if err != nil {
		return nil, err
	}
	rec := core.NewRecord(col)
	rec.Set("sso_groups_claim", "groups")
	return rec, nil
}
