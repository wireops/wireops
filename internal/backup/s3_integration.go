package backup

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/backup/remote"
	"github.com/wireops/wireops/internal/crypto"
)

// s3IntegrationSlug is the "integrations" collection slug for the S3
// storage backend (see internal/integrations/s3) — the same generic
// integrations collection + registry that Vault/Infisical already use for
// "backend service, not container-action" config (see
// internal/secrets/vault.go's BuildVaultClient for the identical lookup
// pattern this mirrors).
const s3IntegrationSlug = "s3"

// s3IntegrationConfig returns the S3 integration's config map with its
// "secret" field decrypted in place, and whether the integration exists and
// is enabled. ok=false (with a nil error) is the normal state for a host
// that hasn't configured remote backup storage — not a failure.
func s3IntegrationConfig(app core.App) (config map[string]any, ok bool, err error) {
	rec, findErr := app.FindFirstRecordByFilter("integrations", "slug = {:slug}", map[string]any{"slug": s3IntegrationSlug})
	if findErr != nil {
		return nil, false, nil
	}
	if !rec.GetBool("enabled") {
		return nil, false, nil
	}

	if err := rec.UnmarshalJSONField("config", &config); err != nil {
		return nil, false, fmt.Errorf("failed to parse s3 integration config: %w", err)
	}
	if config == nil {
		config = map[string]any{}
	}

	if raw, _ := config["secret"].(string); raw != "" {
		plaintext, err := crypto.Decrypt(raw, secretKeyFromEnv())
		if err != nil {
			return nil, false, fmt.Errorf("failed to decrypt s3 integration secret: %w", err)
		}
		config["secret"] = string(plaintext)
	}
	return config, true, nil
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

	if _, err := app.FindFirstRecordByFilter("integrations", "slug = {:slug}", map[string]any{"slug": s3IntegrationSlug}); err == nil {
		return nil
	}

	col, err := app.FindCollectionByNameOrId("integrations")
	if err != nil {
		return fmt.Errorf("failed to find integrations collection: %w", err)
	}

	encryptedSecret, err := crypto.Encrypt([]byte(s3.Secret), secretKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt legacy S3 secret: %w", err)
	}

	rec := core.NewRecord(col)
	rec.Set("slug", s3IntegrationSlug)
	rec.Set("enabled", true)
	rec.Set("config", map[string]any{
		"bucket":           s3.Bucket,
		"region":           s3.Region,
		"endpoint":         s3.Endpoint,
		"prefix":           "",
		"force_path_style": s3.ForcePathStyle,
		"access_key":       s3.AccessKey,
		"secret":           encryptedSecret,
		"encrypt_content":  true,
	})
	if err := app.Save(rec); err != nil {
		return fmt.Errorf("failed to save migrated s3 integration: %w", err)
	}

	settings := app.Settings()
	settings.Backups.S3.Enabled = false
	if err := app.Save(settings); err != nil {
		return fmt.Errorf("failed to disable PocketBase's native S3 backend: %w", err)
	}

	app.Logger().Info("migrated legacy S3 backup settings into the s3 integration and disabled PocketBase's native S3 backend")
	return nil
}
