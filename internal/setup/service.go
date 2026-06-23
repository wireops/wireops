package setup

import (
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/audit"
)

var (
	ErrSetupAlreadyDone = errors.New("setup already done")
	ErrBootstrapFailed  = errors.New("setup bootstrap failed")

	bootstrapMu sync.Mutex
)

type Service struct {
	app             core.App
	afterUserCreate func(*core.Record) error
}

func NewService(app core.App) *Service {
	return &Service{app: app}
}

func (s *Service) CreateInitialAdmin(email, password string) error {
	maskedEmail := MaskEmail(email)
	log.Printf("[setup] bootstrap started for %s", maskedEmail)
	recordSetupAudit(s.app, "setup.bootstrap_started", audit.StatusSuccess, "", maskedEmail)

	bootstrapMu.Lock()
	defer bootstrapMu.Unlock()

	txErr := s.app.RunInTransaction(func(txApp core.App) error {
		count, err := CountRealUsers(txApp)
		if err != nil {
			return err
		}
		if count > 0 {
			return ErrSetupAlreadyDone
		}

		users, err := txApp.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}

		userRecord := core.NewRecord(users)
		userRecord.Set("email", email)
		userRecord.Set("password", password)
		userRecord.Set("role", "admin")
		userRecord.Set("verified", true)
		userRecord.Set("protected", true)
		if err := txApp.Save(userRecord); err != nil {
			return err
		}

		if s.afterUserCreate != nil {
			if err := s.afterUserCreate(userRecord); err != nil {
				return err
			}
		}

		superusers, err := txApp.FindCollectionByNameOrId(core.CollectionNameSuperusers)
		if err != nil {
			return err
		}

		superRecord := core.NewRecord(superusers)
		superRecord.Set("email", email)
		superRecord.Set("verified", true)
		superRecord.Set("passwordHash", userRecord.GetString("passwordHash"))
		superRecord.Set("tokenKey", userRecord.GetString("tokenKey"))
		if err := txApp.SaveNoValidate(superRecord); err != nil {
			return err
		}

		return nil
	})

	if errors.Is(txErr, ErrSetupAlreadyDone) {
		log.Printf("[setup] bootstrap rejected for %s: already configured", maskedEmail)
		recordSetupAudit(s.app, "setup.bootstrap_rejected", audit.StatusError, "already_configured", maskedEmail)
		return ErrSetupAlreadyDone
	}
	if txErr != nil {
		log.Printf("[setup] bootstrap failed for %s: %v", maskedEmail, txErr)
		recordSetupAudit(s.app, "setup.bootstrap_failed", audit.StatusError, "bootstrap_failed", maskedEmail)
		return fmt.Errorf("%w: %v", ErrBootstrapFailed, txErr)
	}

	log.Printf("[setup] bootstrap completed for %s", maskedEmail)
	recordSetupAudit(s.app, "setup.bootstrap_completed", audit.StatusSuccess, "", maskedEmail)
	return nil
}

func CountRealUsers(app core.App) (int64, error) {
	if _, err := app.FindCollectionByNameOrId("users"); err == nil {
		return app.CountRecords("users", dbx.Not(dbx.HashExp{"email": core.DefaultInstallerEmail}))
	}
	return app.CountRecords(core.CollectionNameSuperusers, dbx.Not(dbx.HashExp{"email": core.DefaultInstallerEmail}))
}

func recordSetupAudit(app core.App, action, status, errorCode, maskedEmail string) {
	audit.Record(app, audit.Event{
		ActorType:    audit.ActorAnonymous,
		Action:       action,
		ResourceType: "setup",
		ResourceID:   "initial",
		Origin:       audit.OriginSetup,
		Status:       status,
		ErrorCode:    errorCode,
		Metadata: map[string]any{
			"email_masked": maskedEmail,
		},
	})
}

func MaskEmail(email string) string {
	if email == "" {
		return "[empty]"
	}

	at := -1
	for i := 0; i < len(email); i++ {
		if email[i] == '@' {
			at = i
			break
		}
	}
	if at <= 0 || at >= len(email)-1 {
		return "[invalid]"
	}

	return email[:1] + "***" + email[at:]
}
