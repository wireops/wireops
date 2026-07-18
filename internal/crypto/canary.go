package crypto

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
)

// canaryPlaintext is a fixed marker encrypted under SECRET_KEY and stored in
// the secret_key_canary collection. Decrypting it back to this exact value
// confirms the running SECRET_KEY matches the one that encrypted every other
// secret in this DATA_DIR.
const canaryPlaintext = "wireops-secret-key-canary-v1"

// VerifyOrSeedSecretKeyCanary seeds the canary row on first boot (nothing to
// compare against yet), or verifies SECRET_KEY still decrypts it correctly
// on every subsequent boot. A decrypt failure or mismatch means SECRET_KEY
// no longer matches this DATA_DIR — most commonly because a backup was
// restored onto a host with the wrong SECRET_KEY, which would otherwise only
// surface later as scattered, hard-to-diagnose per-secret decrypt failures.
func VerifyOrSeedSecretKeyCanary(app core.App, secretKey []byte) error {
	if len(secretKey) != 32 {
		return fmt.Errorf("SECRET_KEY canary check: key must be 32 bytes (got %d)", len(secretKey))
	}

	recs, err := app.FindAllRecords("secret_key_canary")
	if err != nil {
		return fmt.Errorf("SECRET_KEY canary check: failed to query secret_key_canary: %w", err)
	}

	if len(recs) == 0 {
		hasExisting, err := hasExistingEncryptedData(app)
		if err != nil {
			return fmt.Errorf("SECRET_KEY canary check: failed to check for existing encrypted data: %w", err)
		}
		if hasExisting {
			return fmt.Errorf("SECRET_KEY canary check: this datastore already holds encrypted secrets but has no secret_key_canary row yet — refusing to silently adopt the current SECRET_KEY as canonical without verifying it. Confirm SECRET_KEY matches what previously encrypted this data, then manually seed a secret_key_canary row (or restore one) to proceed")
		}

		col, err := app.FindCollectionByNameOrId("secret_key_canary")
		if err != nil {
			return fmt.Errorf("SECRET_KEY canary check: secret_key_canary collection missing: %w", err)
		}
		encrypted, err := Encrypt([]byte(canaryPlaintext), secretKey)
		if err != nil {
			return fmt.Errorf("SECRET_KEY canary check: failed to seed canary: %w", err)
		}
		rec := core.NewRecord(col)
		rec.Set("value", encrypted)
		if err := app.Save(rec); err != nil {
			return fmt.Errorf("SECRET_KEY canary check: failed to save canary: %w", err)
		}
		return nil
	}

	decrypted, err := Decrypt(recs[0].GetString("value"), secretKey)
	if err != nil || string(decrypted) != canaryPlaintext {
		return fmt.Errorf("SECRET_KEY does not match this DATA_DIR — encrypted stack secrets (git passwords, SSH keys, integration tokens) are unreadable with the current SECRET_KEY. This usually means a backup was restored onto a host with the wrong SECRET_KEY, or SECRET_KEY was rotated without re-encrypting existing data")
	}
	return nil
}

// secretBearingFields are (collection, field) pairs known to hold
// AES-GCM-encrypted values (see internal/hooks/pb_hooks.go: encryptField,
// ensureRepositorySopsKeypair, prepareEnvSecretRecord). A pre-existing,
// non-empty value in any of these means this datastore already has secrets
// encrypted under some SECRET_KEY, so the canary must not be seeded blindly.
var secretBearingFields = [][2]string{
	{"repository_keys", "ssh_private_key"},
	{"repository_keys", "git_password"},
	{"repositories", "sops_age_key"},
	{"stack_env_vars", "value"},
	{"job_env_vars", "value"},
}

func hasExistingEncryptedData(app core.App) (bool, error) {
	for _, cf := range secretBearingFields {
		collection, field := cf[0], cf[1]
		recs, err := app.FindRecordsByFilter(collection, field+" != ''", "", 1, 0)
		if err != nil {
			// Collection/field may not exist on older schemas mid-migration;
			// treat as no data there rather than failing the whole check.
			continue
		}
		if len(recs) > 0 {
			return true, nil
		}
	}
	return false, nil
}
