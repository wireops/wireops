package routes

import (
	"encoding/json"
	"net/http"
	"net/mail"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

func RegisterSetupRoutes(r *router.Router[*core.RequestEvent], app core.App) {
	r.GET("/api/custom/setup/status", handleSetupStatus(app))
	r.POST("/api/custom/setup", handleSetupCreate(app))
}

func handleSetupStatus(app core.App) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		count, err := countRealSuperusers(app)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to check setup status"})
		}
		return e.JSON(http.StatusOK, map[string]bool{"needsSetup": count == 0})
	}
}

func handleSetupCreate(app core.App) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		// Guard: only allowed when no real superusers exist yet.
		count, err := countRealSuperusers(app)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to check setup status"})
		}
		if count > 0 {
			return e.JSON(http.StatusForbidden, map[string]string{"error": "setup has already been completed"})
		}

		var body struct {
			Email           string `json:"email"`
			Password        string `json:"password"`
			PasswordConfirm string `json:"passwordConfirm"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		}

		if body.Email == "" || body.Password == "" || body.PasswordConfirm == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "email, password and passwordConfirm are required"})
		}
		if _, err := mail.ParseAddress(body.Email); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid email address"})
		}
		if len(body.Password) < 8 {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
		}
		if body.Password != body.PasswordConfirm {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "passwords do not match"})
		}

		superusers, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "internal error"})
		}

		record := core.NewRecord(superusers)
		record.Set("email", body.Email)
		record.Set("password", body.Password)
		if err := app.Save(record); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		return e.JSON(http.StatusCreated, map[string]string{"status": "created"})
	}
}

// countRealSuperusers returns the number of superusers excluding the
// temporary installer account that PocketBase auto-creates on first boot
// (email: __pbinstaller@example.com).
func countRealSuperusers(app core.App) (int64, error) {
	return app.CountRecords(
		core.CollectionNameSuperusers,
		dbx.Not(dbx.HashExp{"email": core.DefaultInstallerEmail}),
	)
}
