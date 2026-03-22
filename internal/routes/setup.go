package routes

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/mail"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

func RegisterSetupRoutes(r *router.Router[*core.RequestEvent], app core.App) {
	r.GET("/api/custom/setup/status", handleSetupStatus(app))
	r.POST("/api/custom/setup", localhostOnly(handleSetupCreate(app)))
}

// localhostOnly is a middleware that restricts a handler to requests originating
// from the loopback interface. This protects the setup endpoint from being
// triggered by a remote client before the legitimate admin has a chance to act.
//
// When wireops is deployed inside Docker with published ports, requests from a
// host browser arrive via the Docker bridge network (not 127.0.0.1). In that
// case bind the port to the host loopback instead: -p 127.0.0.1:8090:8090.
func localhostOnly(next func(*core.RequestEvent) error) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		host, _, err := net.SplitHostPort(e.Request.RemoteAddr)
		if err != nil {
			host = e.Request.RemoteAddr
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return e.JSON(http.StatusForbidden, map[string]string{"error": "setup endpoint is only accessible from localhost"})
		}
		return next(e)
	}
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

		// Run the guard-check and record creation in a single transaction to
		// prevent concurrent requests from creating multiple admin accounts.
		txErr := app.RunInTransaction(func(txApp core.App) error {
			count, err := countRealSuperusers(txApp)
			if err != nil {
				// Propagate DB errors unchanged so the outer handler maps them
				// to a 500 rather than a misleading 403.
				return err
			}
			if count > 0 {
				return errSetupAlreadyDone
			}

			superusers, err := txApp.FindCollectionByNameOrId(core.CollectionNameSuperusers)
			if err != nil {
				return err
			}

			record := core.NewRecord(superusers)
			record.Set("email", body.Email)
			record.Set("password", body.Password)
			return txApp.Save(record)
		})

		if txErr == errSetupAlreadyDone {
			return e.JSON(http.StatusForbidden, map[string]string{"error": "setup has already been completed"})
		}
		if txErr != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "internal error"})
		}

		return e.JSON(http.StatusCreated, map[string]string{"status": "created"})
	}
}

// errSetupAlreadyDone is a sentinel returned inside the transaction to signal
// that a real superuser already exists so we can map it to a 403 response.
var errSetupAlreadyDone = errors.New("setup already done")

// countRealSuperusers returns the number of superusers excluding the
// temporary installer account that PocketBase auto-creates on first boot
// (email: __pbinstaller@example.com).
func countRealSuperusers(app core.App) (int64, error) {
	return app.CountRecords(
		core.CollectionNameSuperusers,
		dbx.Not(dbx.HashExp{"email": core.DefaultInstallerEmail}),
	)
}
