package routes

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/rbac"
)

// buildRouteTestZip returns bytes for a real, structurally-valid zip
// archive, mirroring internal/backup.buildTestZip — the upload route runs
// the same structural validation as the service layer.
func buildRouteTestZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("data.txt")
	if err != nil {
		t.Fatalf("failed to add zip entry: %v", err)
	}
	if _, err := w.Write([]byte("test")); err != nil {
		t.Fatalf("failed to write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}
	return buf.Bytes()
}

func backupRoutesMux(t *testing.T, app core.App, auth *core.Record) http.Handler {
	t.Helper()

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:   app,
			Event: router.Event{Response: w, Request: req},
			Auth:  auth,
		}, nil
	})

	rr := routeRegistrar{r: r, app: app}
	rr.registerBackupRoutes()

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return mux
}

func doJSONRequest(t *testing.T, mux http.Handler, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *bytes.Buffer
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, target, reqBody)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestBackupRoutesListCreateDeleteAsAdmin(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	admin := createTestUser(t, app, "admin@example.com", "Password1!", rbac.RoleAdmin)
	mux := backupRoutesMux(t, app, admin)

	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/backups", map[string]string{"filename": "route_test.zip"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/custom/backups: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSONRequest(t, mux, http.MethodGet, "/api/custom/backups", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/custom/backups: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var list []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	found := false
	for _, item := range list {
		if item["key"] == "route_test.zip" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected route_test.zip in list, got %+v", list)
	}

	rec = doJSONRequest(t, mux, http.MethodDelete, "/api/custom/backups/route_test.zip", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBackupRoutesDenyWithoutCapManageSettings(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	viewer := createTestUser(t, app, "viewer@example.com", "Password1!", rbac.RoleViewer)
	mux := backupRoutesMux(t, app, viewer)

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/backups", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer role, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBackupRoutesSettingsRoundTrip(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	admin := createTestUser(t, app, "admin@example.com", "Password1!", rbac.RoleAdmin)
	mux := backupRoutesMux(t, app, admin)

	rec := doJSONRequest(t, mux, http.MethodPut, "/api/custom/backups/settings", map[string]any{
		"cron":          "0 3 * * *",
		"cron_max_keep": 4,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT settings: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSONRequest(t, mux, http.MethodGet, "/api/custom/backups/settings", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET settings: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	if got["cron"] != "0 3 * * *" {
		t.Fatalf("expected cron to persist, got %+v", got)
	}
}

func TestBackupRoutesSettingsRejectsMalformedBody(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	admin := createTestUser(t, app, "admin@example.com", "Password1!", rbac.RoleAdmin)
	mux := backupRoutesMux(t, app, admin)

	req := httptest.NewRequest(http.MethodPut, "/api/custom/backups/settings", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed body, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBackupRoutesRestoreRequiresConfirm(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	admin := createTestUser(t, app, "admin@example.com", "Password1!", rbac.RoleAdmin)
	mux := backupRoutesMux(t, app, admin)

	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/backups", map[string]string{"filename": "restore_test.zip"})
	if rec.Code != http.StatusOK {
		t.Fatalf("setup create: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSONRequest(t, mux, http.MethodPost, "/api/custom/backups/restore_test.zip/restore", map[string]bool{"confirm": false})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without confirm, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBackupRoutesSyncLocalNoopForAlreadyLocalBackup(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	admin := createTestUser(t, app, "admin@example.com", "Password1!", rbac.RoleAdmin)
	mux := backupRoutesMux(t, app, admin)

	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/backups", map[string]string{"filename": "sync_local_test.zip"})
	if rec.Code != http.StatusOK {
		t.Fatalf("setup create: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSONRequest(t, mux, http.MethodPost, "/api/custom/backups/sync_local_test.zip/sync-local", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for an already-local backup, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBackupRoutesSyncLocalErrorsWhenNotFoundAnywhere(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	admin := createTestUser(t, app, "admin@example.com", "Password1!", rbac.RoleAdmin)
	mux := backupRoutesMux(t, app, admin)

	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/backups/does_not_exist.zip/sync-local", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a backup that exists nowhere, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBackupRoutesUploadRequiresRealSuperuser(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	admin := createTestUser(t, app, "admin@example.com", "Password1!", rbac.RoleAdmin)
	mux := backupRoutesMux(t, app, admin)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "uploaded.zip")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(buildRouteTestZip(t)); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/custom/backups/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for wireops admin (not a real superuser), got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBackupRoutesUploadSucceedsForRealSuperuser(t *testing.T) {
	app := newSetupTestApp(t)
	superuser := createTestSuperuser(t, app, "root@example.com", "Password1!")
	mux := backupRoutesMux(t, app, superuser)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "uploaded.zip")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(buildRouteTestZip(t)); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/custom/backups/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for real superuser upload, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBackupRoutesUploadEnforcesSizeLimit(t *testing.T) {
	t.Setenv("BACKUP_UPLOAD_MAX_MB", "0") // falls back to default cap via config, but we exceed it with a tiny override below
	app := newSetupTestApp(t)
	superuser := createTestSuperuser(t, app, "root@example.com", "Password1!")
	t.Setenv("BACKUP_UPLOAD_MAX_MB", "1")
	mux := backupRoutesMux(t, app, superuser)

	oversized := make([]byte, 2*1024*1024) // 2MB body, over the 1MB configured limit
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "big.zip")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(oversized); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/custom/backups/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("expected upload over the configured size limit to be rejected, got 200: %s", rec.Body.String())
	}
}
