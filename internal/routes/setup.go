package routes

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"net/mail"
	"os"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/audit"
	setupsvc "github.com/wireops/wireops/internal/setup"
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
	count, err := setupsvc.CountRealUsers(app)
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
	bootstrapService := setupsvc.NewService(app)

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

		maskedEmail := setupsvc.MaskEmail(body.Email)

		status, err := currentSetupStatus(app)
		if err != nil {
			log.Printf("[setup] failed to determine setup status before bootstrap for %q: %v", maskedEmail, err)
			recordSetupRouteAudit(app, "setup.bootstrap_failed", audit.StatusError, "status_unknown", body.Email)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "internal error"})
		}
		if !status.NeedsSetup {
			recordSetupRouteAudit(app, "setup.bootstrap_rejected", audit.StatusError, "already_configured", body.Email)
			return e.JSON(http.StatusForbidden, map[string]string{"error": "setup has already been completed"})
		}
		if status.Reason == "missing_bootstrap_token" {
			recordSetupRouteAudit(app, "setup.bootstrap_rejected", audit.StatusError, "missing_bootstrap_token", body.Email)
			return e.JSON(http.StatusForbidden, map[string]string{"error": "bootstrap token is not configured"})
		}
		if subtle.ConstantTimeCompare([]byte(body.BootstrapToken), []byte(os.Getenv("BOOTSTRAP_TOKEN"))) != 1 {
			recordSetupRouteAudit(app, "setup.bootstrap_rejected", audit.StatusError, "invalid_bootstrap_token", body.Email)
			return e.JSON(http.StatusForbidden, map[string]string{"error": "invalid bootstrap token"})
		}

		if err := bootstrapService.CreateInitialAdmin(body.Email, body.Password); err != nil {
			if err == setupsvc.ErrSetupAlreadyDone {
				return e.JSON(http.StatusForbidden, map[string]string{"error": "setup has already been completed"})
			}
			log.Printf("[setup] failed to create initial admin for %q: %v", maskedEmail, err)
			recordSetupRouteAudit(app, "setup.bootstrap_failed", audit.StatusError, "internal_error", body.Email)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "internal error"})
		}

		return e.JSON(http.StatusCreated, map[string]string{"status": "created"})
	}
}

func recordSetupRouteAudit(app core.App, action, status, errorCode, email string) {
	audit.Record(app, audit.Event{
		ActorType:    audit.ActorAnonymous,
		Action:       action,
		ResourceType: "setup",
		ResourceID:   "initial",
		Origin:       audit.OriginSetup,
		Status:       status,
		ErrorCode:    errorCode,
		Metadata: map[string]any{
			"email_masked": setupsvc.MaskEmail(email),
		},
	})
}
