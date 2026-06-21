package routes

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/mail"
	"os"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

func RegisterSetupRoutes(r *router.Router[*core.RequestEvent], app core.App) {
	r.GET("/api/custom/setup/status", handleSetupStatus(app))
	r.POST("/api/custom/setup", handleSetupCreate(app))
}

type setupStatus struct {
	NeedsSetup             bool   `json:"needsSetup"`
	SetupAllowed           bool   `json:"setupAllowed"`
	Reason                 string `json:"reason"`
	RequiresBootstrapToken bool   `json:"requiresBootstrapToken"`
}

func currentSetupStatus(app core.App) (setupStatus, error) {
	count, err := countRealUsers(app)
	if err != nil {
		return setupStatus{
			NeedsSetup:             false,
			SetupAllowed:           false,
			Reason:                 "unknown",
			RequiresBootstrapToken: false,
		}, err
	}

	if count > 0 {
		return setupStatus{
			NeedsSetup:             false,
			SetupAllowed:           false,
			Reason:                 "already_configured",
			RequiresBootstrapToken: false,
		}, nil
	}

	token := os.Getenv("BOOTSTRAP_TOKEN")
	if token == "" {
		return setupStatus{
			NeedsSetup:             true,
			SetupAllowed:           false,
			Reason:                 "missing_bootstrap_token",
			RequiresBootstrapToken: true,
		}, nil
	}

	return setupStatus{
		NeedsSetup:             true,
		SetupAllowed:           true,
		Reason:                 "",
		RequiresBootstrapToken: true,
	}, nil
}

func handleSetupStatus(app core.App) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		status, err := currentSetupStatus(app)
		if err != nil {
			log.Printf("[setup] failed to determine setup status: %v", err)
			return e.JSON(http.StatusInternalServerError, status)
		}
		return e.JSON(http.StatusOK, status)
	}
}

func handleSetupCreate(app core.App) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		var body struct {
			Email          string `json:"email"`
			Password       string `json:"password"`
			BootstrapToken string `json:"bootstrapToken"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		}

		if body.Email == "" || body.Password == "" || body.BootstrapToken == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "email, password and bootstrapToken are required"})
		}
		if _, err := mail.ParseAddress(body.Email); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid email address"})
		}
		if len(body.Password) < 8 {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
		}

		status, err := currentSetupStatus(app)
		if err != nil {
			log.Printf("[setup] failed to determine setup status before bootstrap for %q: %v", body.Email, err)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "internal error"})
		}
		if !status.NeedsSetup {
			return e.JSON(http.StatusForbidden, map[string]string{"error": "setup has already been completed"})
		}
		if status.Reason == "missing_bootstrap_token" {
			return e.JSON(http.StatusForbidden, map[string]string{"error": "bootstrap token is not configured"})
		}
		if subtle.ConstantTimeCompare([]byte(body.BootstrapToken), []byte(os.Getenv("BOOTSTRAP_TOKEN"))) != 1 {
			return e.JSON(http.StatusForbidden, map[string]string{"error": "invalid bootstrap token"})
		}

		// Run the guard-check and record creation in a single transaction to
		// prevent concurrent requests from creating multiple admin accounts.
		txErr := app.RunInTransaction(func(txApp core.App) error {
			count, err := countRealUsers(txApp)
			if err != nil {
				// Propagate DB errors unchanged so the outer handler maps them
				// to a 500 rather than a misleading 403.
				return err
			}
			if count > 0 {
				return errSetupAlreadyDone
			}

			users, err := txApp.FindCollectionByNameOrId("users")
			if err != nil {
				return err
			}

			record := core.NewRecord(users)
			record.Set("email", body.Email)
			record.Set("password", body.Password)
			record.Set("role", "admin")
			record.Set("verified", true)
			record.Set("protected", true)
			if err := txApp.Save(record); err != nil {
				return err
			}

			superusers, err := txApp.FindCollectionByNameOrId(core.CollectionNameSuperusers)
			if err != nil {
				return err
			}
			superRecord := core.NewRecord(superusers)
			superRecord.Set("email", body.Email)
			superRecord.Set("password", body.Password)
			return txApp.Save(superRecord)
		})

		if txErr == errSetupAlreadyDone {
			return e.JSON(http.StatusForbidden, map[string]string{"error": "setup has already been completed"})
		}
		if txErr != nil {
			log.Printf("[setup] failed to create initial admin for %q: %v", body.Email, txErr)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "internal error"})
		}

		return e.JSON(http.StatusCreated, map[string]string{"status": "created"})
	}
}

// errSetupAlreadyDone is a sentinel returned inside the transaction to signal
// that a real superuser already exists so we can map it to a 403 response.
var errSetupAlreadyDone = errors.New("setup already done")

// countRealUsers returns the number of RBAC users excluding the
// temporary installer account that PocketBase auto-creates on first boot
// (email: __pbinstaller@example.com).
func countRealUsers(app core.App) (int64, error) {
	if _, err := app.FindCollectionByNameOrId("users"); err == nil {
		return app.CountRecords("users", dbx.Not(dbx.HashExp{"email": core.DefaultInstallerEmail}))
	}
	return app.CountRecords(core.CollectionNameSuperusers, dbx.Not(dbx.HashExp{"email": core.DefaultInstallerEmail}))
}
