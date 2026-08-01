package pb_migrations

import (
	"database/sql"
	"errors"
	"log"
	"os"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/wireops/wireops/internal/crypto"
)

// Migration 63: encrypt-at-rest for discord/slack webhook URLs and
// ntfy/webhook secrets.
//
// Before the descriptor-driven integrations refactor, these four
// notification providers deliberately stored their sensitive field
// plaintext (see the removed routes_register.go encryptedIntegrationConfigKeys
// comment: "webhook/ntfy/discord/slack sensitive values remain plaintext at
// rest"). Their descriptors now mark that field Encrypted: true
// (internal/integrations/{discord,slack,ntfy,webhook}), so
// internal/integrations.Store.Load unconditionally tries to AES-GCM decrypt
// it. Any row written before this migration is still plaintext and would
// fail to decrypt, breaking the whole /api/custom/integrations list and
// silently disabling notification dispatch for that provider. This
// migration encrypts every pre-existing row's plaintext value exactly once.
var notifierSecretFieldBySlug = map[string]string{
	"discord": "url",
	"slack":   "url",
	"ntfy":    "secret",
	"webhook": "secret",
}

func init() {
	m.Register(func(app core.App) error {
		return encryptNotifierIntegrationSecrets(app, crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY")))
	}, func(app core.App) error {
		// Rollback is a no-op: reverting to plaintext would re-introduce the
		// bug this migration fixes, and Store only knows how to decrypt
		// Encrypted fields going forward regardless of this row's history.
		return nil
	})
}

func encryptNotifierIntegrationSecrets(app core.App, secretKey []byte) error {
	if secretKey == nil {
		log.Println("[MIGRATE] Skipping notifier secret encryption backfill: SECRET_KEY is not set or invalid")
		return nil
	}

	for slug, field := range notifierSecretFieldBySlug {
		rec, err := app.FindFirstRecordByFilter("integrations", "slug = {:slug}", map[string]any{"slug": slug})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue // no row for this slug yet — nothing to migrate
			}
			return err
		}

		var cfg map[string]any
		if err := rec.UnmarshalJSONField("config", &cfg); err != nil {
			return err
		}
		val, ok := cfg[field].(string)
		if !ok || val == "" {
			continue
		}
		if _, err := crypto.Decrypt(val, secretKey); err == nil {
			// Already ciphertext (e.g. a down+up replay) — leave it alone,
			// re-encrypting would double-wrap it.
			continue
		}

		encrypted, err := crypto.Encrypt([]byte(val), secretKey)
		if err != nil {
			return err
		}
		cfg[field] = encrypted
		rec.Set("config", cfg)
		if err := app.Save(rec); err != nil {
			return err
		}
		log.Printf("[MIGRATE] Encrypted %s.%s at rest", slug, field)
	}

	return nil
}
