package integrations_test

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/wireops/wireops/internal/integrations"
)

const testStoreSecretKey = "cccccccccccccccccccccccccccccc32" // exactly 32 bytes

// newIntegrationsStoreTestApp returns a test PocketBase app with a minimal
// integrations collection, matching pb_migrations/01_init_collections.go's
// createIntegrations — mirrors internal/secrets/test_helpers_test.go's
// newSecretBackendsTestApp and internal/backup's newS3TestApp, since Store
// is now the one place this fixture shape actually needs to live, but each
// consumer package still needs its own copy (test-only files aren't
// importable across packages).
func newIntegrationsStoreTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	col := core.NewBaseCollection("integrations")
	col.Fields.Add(&core.TextField{Name: "slug", Required: true})
	col.Fields.Add(&core.BoolField{Name: "enabled"})
	col.Fields.Add(&core.BoolField{Name: "locked"})
	col.Fields.Add(&core.JSONField{Name: "config"})
	if err := app.Save(col); err != nil {
		t.Fatalf("save integrations collection: %v", err)
	}

	return app
}

// TestStoreRoundTripsEveryEncryptedField saves and reloads one representative
// Encrypted field per slug that declares one, asserting Load hands back the
// plaintext value and the raw stored value is not the plaintext (i.e.
// actually encrypted at rest, not just passed through).
func TestStoreRoundTripsEveryEncryptedField(t *testing.T) {
	cases := []struct {
		slug   string
		field  string
		value  string
		config map[string]any
	}{
		{"vault", "token", "s.mytoken", map[string]any{"address": "https://vault.example.com", "token": "s.mytoken"}},
		{"infisical", "client_secret", "csecret", map[string]any{"client_id": "cid", "client_secret": "csecret"}},
		{"s3", "secret", "s3cr3t", map[string]any{"bucket": "b", "region": "r", "access_key": "ak", "secret": "s3cr3t"}},
		{"discord", "url", "https://discord.com/api/webhooks/1/abc", map[string]any{"url": "https://discord.com/api/webhooks/1/abc"}},
		{"slack", "url", "https://hooks.slack.com/services/T/B/xxx", map[string]any{"url": "https://hooks.slack.com/services/T/B/xxx"}},
		{"webhook", "secret", "hmac-secret", map[string]any{"url": "https://hooks.example.com", "secret": "hmac-secret"}},
		{"ntfy", "secret", "ntfy-token", map[string]any{"topic": "x", "secret": "ntfy-token"}},
	}

	for _, tc := range cases {
		t.Run(tc.slug, func(t *testing.T) {
			app := newIntegrationsStoreTestApp(t)
			store := integrations.NewStore(app, []byte(testStoreSecretKey))

			if err := store.Save(tc.slug, true, tc.config); err != nil {
				t.Fatalf("save %s: %v", tc.slug, err)
			}

			recs, err := app.FindAllRecords("integrations")
			if err != nil {
				t.Fatalf("query raw record: %v", err)
			}
			if len(recs) != 1 {
				t.Fatalf("expected exactly one integrations row, found %d", len(recs))
			}
			var rawCfg map[string]any
			if err := recs[0].UnmarshalJSONField("config", &rawCfg); err != nil {
				t.Fatalf("unmarshal raw config: %v", err)
			}
			rawVal, _ := rawCfg[tc.field].(string)
			if rawVal == tc.value {
				t.Fatalf("%s.%s was not encrypted at rest — stored value equals plaintext", tc.slug, tc.field)
			}

			instance, err := store.Load(tc.slug)
			if err != nil {
				t.Fatalf("load %s: %v", tc.slug, err)
			}
			got, _ := instance.Config[tc.field].(string)
			if got != tc.value {
				t.Fatalf("Load %s.%s = %q, want %q", tc.slug, tc.field, got, tc.value)
			}
		})
	}
}

// TestStoreSaveRejectsInvalidSecretKey guards against silently persisting a
// Sensitive+Encrypted field in plaintext when SECRET_KEY is missing or
// malformed.
func TestStoreSaveRejectsInvalidSecretKey(t *testing.T) {
	app := newIntegrationsStoreTestApp(t)

	store := integrations.NewStore(app, nil)
	vaultCfg := map[string]any{"address": "https://vault.example.com", "token": "s.mytoken"}
	if err := store.Save("vault", true, vaultCfg); err == nil {
		t.Fatal("expected error saving vault config with a nil secret key")
	}

	store = integrations.NewStore(app, []byte("too-short"))
	infisicalCfg := map[string]any{"client_id": "cid", "client_secret": "csecret"}
	if err := store.Save("infisical", true, infisicalCfg); err == nil {
		t.Fatal("expected error saving infisical config with a non-32-byte secret key")
	}

	// A slug whose submitted config has nothing in an Encrypted field present
	// must not require a valid secret key — webhook's own "secret" is
	// Encrypted, but omitting it here means Save's encrypt loop never runs.
	webhookCfg := map[string]any{"url": "https://hooks.example.com"}
	if err := store.Save("webhook", true, webhookCfg); err != nil {
		t.Fatalf("webhook config without a secret must save without a valid secret key: %v", err)
	}
}

// TestStoreMaskedResubmitDoesNotDoubleEncrypt is the direct regression proof
// for the trap the old alreadyEncryptedKeys bookkeeping existed to avoid:
// resubmitting the masked sentinel for a Sensitive field must carry the
// existing stored ciphertext through byte-identical, never re-encrypt it (or
// worse, encrypt the literal sentinel string).
func TestStoreMaskedResubmitDoesNotDoubleEncrypt(t *testing.T) {
	app := newIntegrationsStoreTestApp(t)
	store := integrations.NewStore(app, []byte(testStoreSecretKey))

	if err := store.Save("vault", true, map[string]any{
		"address": "https://vault.example.com",
		"token":   "s.mytoken",
	}); err != nil {
		t.Fatalf("first save: %v", err)
	}

	firstRaw := rawStoredConfig(t, app, "vault")
	firstCiphertext, _ := firstRaw["token"].(string)
	if firstCiphertext == "" || firstCiphertext == "s.mytoken" {
		t.Fatalf("token was not encrypted after first save: %q", firstCiphertext)
	}

	loaded, err := store.Load("vault")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	masked := store.Mask("vault", loaded.Config)
	maskedToken, _ := masked["token"].(string)
	if maskedToken != "••••••••" {
		t.Fatalf("expected masked token, got %q", maskedToken)
	}

	// Resubmit the masked value unchanged, as the PUT handler does when the
	// operator didn't touch the token field.
	if err := store.Save("vault", true, map[string]any{
		"address": "https://vault.example.com",
		"token":   maskedToken,
	}); err != nil {
		t.Fatalf("second save (masked resubmit): %v", err)
	}

	secondRaw := rawStoredConfig(t, app, "vault")
	secondCiphertext, _ := secondRaw["token"].(string)
	if secondCiphertext != firstCiphertext {
		t.Fatalf("masked resubmit changed the stored ciphertext: first=%q second=%q", firstCiphertext, secondCiphertext)
	}
}

func rawStoredConfig(t *testing.T, app core.App, slug string) map[string]any {
	t.Helper()
	recs, err := app.FindAllRecords("integrations")
	if err != nil {
		t.Fatalf("query raw records: %v", err)
	}
	for _, rec := range recs {
		if rec.GetString("slug") != slug {
			continue
		}
		var cfg map[string]any
		if err := rec.UnmarshalJSONField("config", &cfg); err != nil {
			t.Fatalf("unmarshal raw config: %v", err)
		}
		return cfg
	}
	t.Fatalf("no stored row found for slug %q", slug)
	return nil
}
