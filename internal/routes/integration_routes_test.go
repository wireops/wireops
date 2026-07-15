package routes

import (
	"testing"

	"github.com/wireops/wireops/internal/crypto"
)

const testIntegrationEncryptKey = "cccccccccccccccccccccccccccccc32" // exactly 32 bytes

// Vault/Infisical connection config is folded into the generic "integrations"
// collection/routes, but unlike webhook/ntfy/discord/slack (which stay
// plaintext at rest, matching pre-existing behavior), vault's token and
// infisical's client_secret grant direct read access to an external secret
// backend and must be encrypted at rest. These tests guard that scoping.
func TestEncryptIntegrationConfigAppliesOnlyToVaultAndInfisical(t *testing.T) {
	key := []byte(testIntegrationEncryptKey)

	vaultCfg := map[string]interface{}{"address": "https://vault.example.com", "token": "s.mytoken"}
	if err := encryptIntegrationConfig("vault", vaultCfg, key, nil); err != nil {
		t.Fatalf("encrypt vault config: %v", err)
	}
	if vaultCfg["token"] == "s.mytoken" {
		t.Fatal("vault token was not encrypted at rest")
	}
	if !crypto.IsEncrypted(vaultCfg["token"].(string)) {
		t.Fatal("vault token does not look encrypted")
	}

	infisicalCfg := map[string]interface{}{"client_id": "cid", "client_secret": "csecret"}
	if err := encryptIntegrationConfig("infisical", infisicalCfg, key, nil); err != nil {
		t.Fatalf("encrypt infisical config: %v", err)
	}
	if infisicalCfg["client_secret"] == "csecret" {
		t.Fatal("infisical client_secret was not encrypted at rest")
	}

	// A client_secret that happens to be valid-base64-alphabet at a length
	// divisible by 4 (e.g. a 64-hex-char token) must still get encrypted —
	// this used to false-positive as "already encrypted" under the old
	// crypto.IsEncrypted content-sniffing check and silently stay plaintext.
	hexLikeCfg := map[string]interface{}{"client_id": "cid", "client_secret": "02366be08f0af095449c602e2026d68e4d6af67541b7557950fd1d510bd4725e"}
	if err := encryptIntegrationConfig("infisical", hexLikeCfg, key, nil); err != nil {
		t.Fatalf("encrypt hex-like infisical config: %v", err)
	}
	if hexLikeCfg["client_secret"] == "02366be08f0af095449c602e2026d68e4d6af67541b7557950fd1d510bd4725e" {
		t.Fatal("hex-like client_secret was not encrypted at rest")
	}

	webhookCfg := map[string]interface{}{"url": "https://hooks.example.com", "secret": "hmac-secret"}
	if err := encryptIntegrationConfig("webhook", webhookCfg, key, nil); err != nil {
		t.Fatalf("encrypt webhook config: %v", err)
	}
	if webhookCfg["secret"] != "hmac-secret" {
		t.Fatal("webhook secret must stay plaintext at rest (unchanged pre-existing behavior)")
	}

	ntfyCfg := map[string]interface{}{"topic": "x", "secret": "ntfy-token"}
	if err := encryptIntegrationConfig("ntfy", ntfyCfg, key, nil); err != nil {
		t.Fatalf("encrypt ntfy config: %v", err)
	}
	if ntfyCfg["secret"] != "ntfy-token" {
		t.Fatal("ntfy secret must stay plaintext at rest (unchanged pre-existing behavior)")
	}
}

// TestEncryptIntegrationConfigSkipsAlreadyEncryptedKeys covers the contract
// that replaced the old content-sniffing idempotency check: callers must
// name which keys were carried over from storage (already ciphertext) via
// alreadyEncryptedKeys, and only those are skipped — encryptIntegrationConfig
// itself no longer guesses from the value's shape.
func TestEncryptIntegrationConfigSkipsAlreadyEncryptedKeys(t *testing.T) {
	key := []byte(testIntegrationEncryptKey)
	cfg := map[string]interface{}{"address": "https://vault.example.com", "token": "s.mytoken"}

	if err := encryptIntegrationConfig("vault", cfg, key, nil); err != nil {
		t.Fatalf("first encrypt: %v", err)
	}
	firstEncrypted := cfg["token"].(string)

	if err := encryptIntegrationConfig("vault", cfg, key, map[string]bool{"token": true}); err != nil {
		t.Fatalf("second encrypt: %v", err)
	}
	if cfg["token"] != firstEncrypted {
		t.Fatal("a key marked as already-encrypted must not be re-encrypted")
	}
}

// TestEncryptIntegrationConfigRejectsInvalidSecretKey guards against
// silently persisting a vault/infisical secret in plaintext when SECRET_KEY
// is missing or malformed — encryptIntegrationConfig used to just skip
// encryption and return nil in that case instead of failing the save.
func TestEncryptIntegrationConfigRejectsInvalidSecretKey(t *testing.T) {
	vaultCfg := map[string]interface{}{"address": "https://vault.example.com", "token": "s.mytoken"}
	if err := encryptIntegrationConfig("vault", vaultCfg, nil, nil); err == nil {
		t.Fatal("expected error encrypting vault config with a nil secret key")
	}
	if vaultCfg["token"] != "s.mytoken" {
		t.Fatal("vault token must not be persisted when encryption fails")
	}

	shortKey := []byte("too-short")
	infisicalCfg := map[string]interface{}{"client_id": "cid", "client_secret": "csecret"}
	if err := encryptIntegrationConfig("infisical", infisicalCfg, shortKey, nil); err == nil {
		t.Fatal("expected error encrypting infisical config with a non-32-byte secret key")
	}
	if infisicalCfg["client_secret"] != "csecret" {
		t.Fatal("infisical client_secret must not be persisted when encryption fails")
	}

	// Non-vault/infisical integrations have nothing that needs encrypting, so
	// an invalid key must not block their save.
	webhookCfg := map[string]interface{}{"url": "https://hooks.example.com", "secret": "hmac-secret"}
	if err := encryptIntegrationConfig("webhook", webhookCfg, nil, nil); err != nil {
		t.Fatalf("webhook config must save without a secret key: %v", err)
	}
}

func TestValidateRequiredIntegrationConfig(t *testing.T) {
	if err := validateRequiredIntegrationConfig("vault", map[string]interface{}{"address": "x"}); err == nil {
		t.Fatal("expected error when vault token is missing")
	}
	if err := validateRequiredIntegrationConfig("vault", map[string]interface{}{"address": "x", "token": "y"}); err != nil {
		t.Fatalf("expected no error with both required vault fields, got %v", err)
	}
	if err := validateRequiredIntegrationConfig("infisical", map[string]interface{}{"client_id": "x"}); err == nil {
		t.Fatal("expected error when infisical client_secret is missing")
	}
	if err := validateRequiredIntegrationConfig("webhook", map[string]interface{}{}); err != nil {
		t.Fatalf("webhook has no required keys, expected nil, got %v", err)
	}

	// allowed_mount / allowed_project_id are optional scoping fields (empty =
	// unrestricted) — enabling vault/infisical without them must still pass.
	if err := validateRequiredIntegrationConfig("vault", map[string]interface{}{"address": "x", "token": "y"}); err != nil {
		t.Fatalf("vault without allowed_mount should still validate, got %v", err)
	}
	if err := validateRequiredIntegrationConfig("infisical", map[string]interface{}{"client_id": "x", "client_secret": "y"}); err != nil {
		t.Fatalf("infisical without allowed_project_id should still validate, got %v", err)
	}
}

// TestScopingFieldsExcludedFromSensitiveAndRequiredKeys documents that
// allowed_mount/allowed_project_id are plain, optional config — never masked,
// never encrypted, never required.
func TestScopingFieldsExcludedFromSensitiveAndRequiredKeys(t *testing.T) {
	for _, key := range sensitiveIntegrationConfigKeys("vault") {
		if key == "allowed_mount" {
			t.Fatal("allowed_mount must not be treated as sensitive")
		}
	}
	for _, key := range encryptedIntegrationConfigKeys("vault") {
		if key == "allowed_mount" {
			t.Fatal("allowed_mount must not be encrypted at rest")
		}
	}
	for _, key := range requiredIntegrationConfigKeys("vault") {
		if key == "allowed_mount" {
			t.Fatal("allowed_mount must not be required")
		}
	}

	for _, key := range sensitiveIntegrationConfigKeys("infisical") {
		if key == "allowed_project_id" {
			t.Fatal("allowed_project_id must not be treated as sensitive")
		}
	}
	for _, key := range encryptedIntegrationConfigKeys("infisical") {
		if key == "allowed_project_id" {
			t.Fatal("allowed_project_id must not be encrypted at rest")
		}
	}
	for _, key := range requiredIntegrationConfigKeys("infisical") {
		if key == "allowed_project_id" {
			t.Fatal("allowed_project_id must not be required")
		}
	}
}
