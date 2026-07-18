package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/audit"
	"github.com/wireops/wireops/internal/backup"
	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/safepath"
)

// recordBackupAudit logs a backup lifecycle action (create/upload/delete/
// restore) to the audit trail. These are destructive/security-sensitive
// enough that "who did what, when, success or failure" needs to be
// reconstructable after the fact — unlike most routes here, both outcomes
// are logged, since a rejected restore/upload attempt is itself signal.
func recordBackupAudit(app core.App, e *core.RequestEvent, action, key string, err error) {
	ev := audit.Event{
		Action:       action,
		ResourceType: "backup",
		ResourceID:   key,
		Status:       audit.StatusSuccess,
	}
	if err != nil {
		ev.Status = audit.StatusError
		ev.ErrorCode = err.Error()
	}
	audit.RecordRequest(app, e, ev)
}

// registerBackupRoutes exposes PocketBase's built-in backup primitives
// (internal/backup wraps core.App.CreateBackup/RestoreBackup and
// app.Settings().Backups) behind wireops's own RBAC, so wireops admins who
// aren't PocketBase superusers can manage backups without the native /_/
// dashboard or /api/backups API.
func (rr routeRegistrar) registerBackupRoutes() {
	rr.r.GET("/api/custom/backups", func(e *core.RequestEvent) error {
		list, err := backup.List(rr.app)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, list)
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	rr.r.POST("/api/custom/backups", func(e *core.RequestEvent) error {
		var body struct {
			Filename string `json:"filename"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil && err.Error() != "EOF" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}

		name := strings.TrimSpace(body.Filename)
		if name == "" {
			name = fmt.Sprintf("wireops_backup_%s_%d.zip", uuid.NewString()[:8], time.Now().Unix())
		}
		name = filepath.Base(name)
		if !strings.HasSuffix(strings.ToLower(name), ".zip") {
			name += ".zip"
		}
		if err := safepath.ValidateBackupKey(name); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}

		if err := backup.Create(e.Request.Context(), rr.app, name); err != nil {
			recordBackupAudit(rr.app, e, "backup.create", name, err)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		recordBackupAudit(rr.app, e, "backup.create", name, nil)

		if e.Request.URL.Query().Get("download") == "true" {
			fsys, err := rr.app.NewBackupsFilesystem()
			if err != nil {
				return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to open backups filesystem"})
			}
			defer fsys.Close()
			return fsys.Serve(e.Response, e.Request, name, name)
		}

		return e.JSON(http.StatusOK, map[string]string{"key": name})
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	// Uploading an arbitrary backup file is deliberately gated to a real
	// PocketBase superuser session (rbac.RequireSuperuser), not just a
	// wireops admin role — see internal/backup.Upload for why. Note the
	// wireops frontend only ever authenticates against the "users"
	// collection, so this is unreachable from the normal UI login; it's
	// meant for operators who separately hold a superuser session (e.g.
	// via the native /_/ dashboard or a superuser API token).
	rr.r.POST("/api/custom/backups/upload", func(e *core.RequestEvent) error {
		files, err := e.FindUploadedFiles("file")
		if err != nil || len(files) == 0 {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing \"file\" upload"})
		}
		name := files[0].OriginalName
		if err := backup.Upload(rr.app, files[0]); err != nil {
			recordBackupAudit(rr.app, e, "backup.upload", name, err)
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		recordBackupAudit(rr.app, e, "backup.upload", name, nil)
		return e.JSON(http.StatusOK, map[string]string{"key": name})
	}).Bind(apis.BodyLimit(config.GetBackupUploadMaxBytes())).BindFunc(rbac.RequireSuperuser())

	rr.r.GET("/api/custom/backups/{key}/download", func(e *core.RequestEvent) error {
		key := e.Request.PathValue("key")
		if err := safepath.ValidateBackupKey(key); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		fsys, err := rr.app.NewBackupsFilesystem()
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to open backups filesystem"})
		}
		defer fsys.Close()
		exists, err := fsys.Exists(key)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to check for backup"})
		}
		if !exists {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "backup not found"})
		}
		return fsys.Serve(e.Response, e.Request, key, key)
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	rr.r.DELETE("/api/custom/backups/{key}", func(e *core.RequestEvent) error {
		key := e.Request.PathValue("key")
		if err := safepath.ValidateBackupKey(key); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		if err := backup.Delete(rr.app, key); err != nil {
			recordBackupAudit(rr.app, e, "backup.delete", key, err)
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		recordBackupAudit(rr.app, e, "backup.delete", key, nil)
		return e.JSON(http.StatusOK, map[string]string{"status": "deleted"})
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	rr.r.POST("/api/custom/backups/{key}/restore", func(e *core.RequestEvent) error {
		key := e.Request.PathValue("key")
		if err := safepath.ValidateBackupKey(key); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}

		var body struct {
			Confirm bool `json:"confirm"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil && err.Error() != "EOF" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		if !body.Confirm {
			err := fmt.Errorf("restore requires \"confirm\": true in the request body")
			recordBackupAudit(rr.app, e, "backup.restore", key, err)
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "restore requires \"confirm\": true in the request body — this is destructive and restarts the server"})
		}

		if err := backup.Restore(e.Request.Context(), rr.app, key, true); err != nil {
			recordBackupAudit(rr.app, e, "backup.restore", key, err)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		recordBackupAudit(rr.app, e, "backup.restore", key, nil)
		return e.JSON(http.StatusOK, map[string]string{"status": "restoring"})
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	// Pulls a remote-only backup (uploaded straight into the bucket, or
	// whose local copy was removed independently) down onto local disk —
	// core.App.RestoreBackup always reads locally, it has no remote
	// fallback of its own, so this must run before restoring such a backup.
	rr.r.POST("/api/custom/backups/{key}/sync-local", func(e *core.RequestEvent) error {
		key := e.Request.PathValue("key")
		if err := safepath.ValidateBackupKey(key); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		if err := backup.SyncLocal(e.Request.Context(), rr.app, key); err != nil {
			recordBackupAudit(rr.app, e, "backup.sync_local", key, err)
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		recordBackupAudit(rr.app, e, "backup.sync_local", key, nil)
		return e.JSON(http.StatusOK, map[string]string{"status": "synced"})
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	rr.r.GET("/api/custom/backups/settings", func(e *core.RequestEvent) error {
		return e.JSON(http.StatusOK, backup.GetSettings(rr.app))
	}).BindFunc(rbac.Require(rbac.CapManageSettings))

	rr.r.PUT("/api/custom/backups/settings", func(e *core.RequestEvent) error {
		var body backup.Settings
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		if err := backup.SaveSettings(rr.app, body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return e.JSON(http.StatusOK, backup.GetSettings(rr.app))
	}).BindFunc(rbac.Require(rbac.CapManageSettings))
}
