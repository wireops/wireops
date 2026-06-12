package routes

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/rbac"
)

func RegisterAuditRoutes(r *router.Router[*core.RequestEvent], app core.App) {
	r.GET("/api/custom/audit-logs", func(e *core.RequestEvent) error {
		q := e.Request.URL.Query()
		page := parsePositiveInt(q.Get("page"), 1)
		perPage := parsePositiveInt(q.Get("perPage"), 25)
		if perPage > 100 {
			perPage = 100
		}

		filter, where, params := auditLogFilters(q.Get("from"), q.Get("to"), q.Get("actor_type"), q.Get("actor_id"), q.Get("action"), q.Get("resource_type"), q.Get("resource_id"), q.Get("origin"), q.Get("status"))
		offset := (page - 1) * perPage

		records, err := app.FindRecordsByFilter("audit_logs", filter, "-created", perPage, offset, params)
		if err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}

		total, err := countAuditLogs(app, where, params)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		items := make([]map[string]any, 0, len(records))
		for _, rec := range records {
			metadata := map[string]any(nil)
			_ = rec.UnmarshalJSONField("metadata_json", &metadata)

			items = append(items, map[string]any{
				"id":            rec.Id,
				"actor_type":    rec.GetString("actor_type"),
				"actor_id":      rec.GetString("actor_id"),
				"action":        rec.GetString("action"),
				"resource_type": rec.GetString("resource_type"),
				"resource_id":   rec.GetString("resource_id"),
				"origin":        rec.GetString("origin"),
				"status":        rec.GetString("status"),
				"error_code":    rec.GetString("error_code"),
				"metadata":      metadata,
				"expires_at":    rec.GetDateTime("expires_at").String(),
				"created":       rec.GetDateTime("created").String(),
			})
		}

		return e.JSON(http.StatusOK, map[string]any{
			"page":       page,
			"perPage":    perPage,
			"totalItems": total,
			"items":      items,
		})
	}).BindFunc(rbac.Require(rbac.CapViewAuditLogs))
}

func parsePositiveInt(raw string, fallback int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func auditLogFilters(from, to, actorType, actorID, action, resourceType, resourceID, origin, status string) (string, string, dbx.Params) {
	var filterParts []string
	var whereParts []string
	params := dbx.Params{}

	add := func(field, op, value, name string) {
		if value == "" {
			return
		}
		filterParts = append(filterParts, field+" "+op+" {:"+name+"}")
		whereParts = append(whereParts, field+" "+op+" {:"+name+"}")
		params[name] = value
	}

	add("created", ">=", strings.TrimSpace(from), "from")
	toVal := strings.TrimSpace(to)
	if len(toVal) == 10 {
		if t, err := time.Parse("2006-01-02", toVal); err == nil {
			add("created", "<", t.AddDate(0, 0, 1).Format("2006-01-02"), "to")
		} else {
			add("created", "<=", toVal, "to")
		}
	} else {
		add("created", "<=", toVal, "to")
	}
	add("actor_type", "=", strings.TrimSpace(actorType), "actor_type")
	add("actor_id", "=", strings.TrimSpace(actorID), "actor_id")
	add("action", "=", strings.TrimSpace(action), "action")
	add("resource_type", "=", strings.TrimSpace(resourceType), "resource_type")
	add("resource_id", "=", strings.TrimSpace(resourceID), "resource_id")
	add("origin", "=", strings.TrimSpace(origin), "origin")
	add("status", "=", strings.TrimSpace(status), "status")

	return strings.Join(filterParts, " && "), strings.Join(whereParts, " AND "), params
}

func countAuditLogs(app core.App, where string, params dbx.Params) (int64, error) {
	query := "SELECT COUNT(*) FROM audit_logs"
	if where != "" {
		query += " WHERE " + where
	}
	var total int64
	err := app.DB().NewQuery(query).Bind(params).Row(&total)
	return total, err
}
