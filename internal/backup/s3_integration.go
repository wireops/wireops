package backup

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/backup/remote"
	"github.com/wireops/wireops/internal/integrations"
)

// s3IntegrationSlug is the "integrations" collection slug for the S3
// storage backend (see internal/integrations/s3) — the same generic
// integrations collection + registry that Vault/Infisical already use for
// "backend service, not container-action" config (see
// internal/secrets/vault.go's BuildVaultClient for the identical lookup
// pattern this mirrors).
const s3IntegrationSlug = "s3"

// s3IntegrationConfig returns the S3 integration's config map (with its
// "secret" field already decrypted by Store.Load) and whether the
// integration exists and is enabled. ok=false (with a nil error) is the
// normal state for a host that hasn't configured remote backup storage —
// not a failure.
func s3IntegrationConfig(app core.App) (config map[string]any, ok bool, err error) {
	store := integrations.NewStore(app, secretKeyFromEnv())
	instance, err := store.Load(s3IntegrationSlug)
	if err != nil {
		return nil, false, err
	}
	if !instance.Enabled {
		return nil, false, nil
	}
	return instance.Config, true, nil
}

func remoteCredentials(config map[string]any) map[string]any {
	return map[string]any{
		"access_key": config["access_key"],
		"secret_key": config["secret"],
	}
}

// remoteEnabled reports whether wireops-owned remote backup storage is
// currently configured and enabled.
func remoteEnabled(app core.App) (bool, error) {
	_, ok, err := s3IntegrationConfig(app)
	return ok, err
}

// buildRemoteStorage constructs a remote.Storage from the enabled S3
// integration's config. Returns an error if it isn't enabled/configured —
// callers should check remoteEnabled first when "not configured" isn't
// itself an error for their flow.
func buildRemoteStorage(app core.App) (remote.Storage, error) {
	config, ok, err := s3IntegrationConfig(app)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("remote backup storage is not enabled")
	}
	return remote.New(s3IntegrationSlug, config, remoteCredentials(config))
}

// buildRemoteKeyManager returns the KMS KeyManager configured for content
// encryption, or nil if KMS isn't enabled (content is then encrypted with
// SECRET_KEY directly instead — see internal/backup/remote/encrypt.go).
func buildRemoteKeyManager(app core.App) (remote.KeyManager, error) {
	config, ok, err := s3IntegrationConfig(app)
	if err != nil || !ok {
		return nil, err
	}
	if enabled, _ := config["kms_enabled"].(bool); !enabled {
		return nil, nil
	}
	return remote.NewKMS("aws_kms", config, remoteCredentials(config))
}

// remoteEncryptContent reports whether backup content should be encrypted
// before upload (independent of which key wraps it — SECRET_KEY or KMS).
func remoteEncryptContent(app core.App) bool {
	config, ok, err := s3IntegrationConfig(app)
	if err != nil || !ok {
		return true
	}
	enabled, _ := config["encrypt_content"].(bool)
	return enabled
}

// MigrateLegacyS3Settings carries over an S3 config that was set through
// PocketBase's own native app.Settings().Backups.S3 (how remote backup
// storage worked before this feature existed) into the "s3" integrations
// row, then disables PocketBase's native S3 backend so it falls back to
// local disk — from this point on, PocketBase only ever manages local
// backups (see internal/backup/remote_ops.go), and the "s3" integration
// takes over the off-host side.
//
// A no-op if PocketBase's native S3 backend was never enabled, or if an
// "s3" integration row already exists (an operator already configured or
// deliberately disabled the new-style integration — don't clobber that).
// Called once at startup, after SECRET_KEY has been validated (see
// cmd/serve.go's OnServe hook, alongside the secret_key_canary check).
func MigrateLegacyS3Settings(app core.App, secretKey []byte) error {
	s3 := app.Settings().Backups.S3
	if !s3.Enabled {
		return nil
	}

	_, err := app.FindFirstRecordByFilter("integrations", "slug = {:slug}", map[string]any{"slug": s3IntegrationSlug})
	alreadyMigrated := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failed to check for existing s3 integration: %w", err)
	}

	return app.RunInTransaction(func(txApp core.App) error {
		if !alreadyMigrated {
			store := integrations.NewStore(txApp, secretKey)
			if err := store.Save(s3IntegrationSlug, true, map[string]any{
				"bucket":           s3.Bucket,
				"region":           s3.Region,
				"endpoint":         s3.Endpoint,
				"prefix":           "",
				"force_path_style": s3.ForcePathStyle,
				"access_key":       s3.AccessKey,
				"secret":           s3.Secret,
				"encrypt_content":  true,
			}); err != nil {
				return fmt.Errorf("failed to save migrated s3 integration: %w", err)
			}
		}

		// Disable PocketBase's native S3 backend regardless of whether the
		// "s3" integration row was just created or already existed — an
		// operator who set up the new-style integration by hand may not
		// have known to also flip off the legacy native backend, and
		// leaving both enabled means backups get pushed through two
		// different S3 clients.
		settings := txApp.Settings()
		settings.Backups.S3.Enabled = false
		if err := txApp.Save(settings); err != nil {
			return fmt.Errorf("failed to disable PocketBase's native S3 backend: %w", err)
		}

		return nil
	})
}
