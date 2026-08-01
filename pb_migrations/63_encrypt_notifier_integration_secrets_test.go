package pb_migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/crypto"
)

func newNotifierSecretsTestApp(t *testing.T) *core.BaseApp {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{
		DataDir:       t.TempDir(),
		EncryptionEnv: "pb_notifier_secrets_migration_test",
	})
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("bootstrap app: %v", err)
	}

	integrations := core.NewBaseCollection("integrations")
	integrations.Fields.Add(&core.TextField{Name: "slug", Required: true})
	integrations.Fields.Add(&core.BoolField{Name: "enabled"})
	integrations.Fields.Add(&core.BoolField{Name: "locked"})
	integrations.Fields.Add(&core.JSONField{Name: "config"})
	if err := app.Save(integrations); err != nil {
		t.Fatalf("save integrations collection: %v", err)
	}
	return app
}

func seedIntegrationRow(t *testing.T, app core.App, slug string, cfg map[string]any) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("integrations")
	if err != nil {
		t.Fatalf("find integrations collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("slug", slug)
	rec.Set("enabled", true)
	rec.Set("config", cfg)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save %s row: %v", slug, err)
	}
}

func rawConfigFor(t *testing.T, app core.App, slug string) map[string]any {
	t.Helper()
	rec, err := app.FindFirstRecordByFilter("integrations", "slug = {:slug}", map[string]any{"slug": slug})
	if err != nil {
		t.Fatalf("find %s row: %v", slug, err)
	}
	var cfg map[string]any
	if err := rec.UnmarshalJSONField("config", &cfg); err != nil {
		t.Fatalf("unmarshal %s config: %v", slug, err)
	}
	return cfg
}

// TestEncryptNotifierIntegrationSecretsEncryptsPreExistingPlaintextRows is the
// direct regression proof for the "Store.Load fails to decrypt a
// pre-migration plaintext row" bug: discord/slack (url) and ntfy/webhook
// (secret) rows saved before this migration must come out encrypted after
// it runs, decryptable back to the original plaintext value.
func TestEncryptNotifierIntegrationSecretsEncryptsPreExistingPlaintextRows(t *testing.T) {
	app := newNotifierSecretsTestApp(t)
	secretKey := []byte("cccccccccccccccccccccccccccccc32") // exactly 32 bytes

	seedIntegrationRow(t, app, "discord", map[string]any{"url": "https://discord.com/api/webhooks/1/abc"})
	seedIntegrationRow(t, app, "slack", map[string]any{"url": "https://hooks.slack.com/services/T/B/xxx"})
	seedIntegrationRow(t, app, "ntfy", map[string]any{"topic": "x", "secret": "ntfy-token"})
	seedIntegrationRow(t, app, "webhook", map[string]any{"url": "https://hooks.example.com", "secret": "hmac-secret"})

	if err := encryptNotifierIntegrationSecrets(app, secretKey); err != nil {
		t.Fatalf("encryptNotifierIntegrationSecrets: %v", err)
	}

	cases := []struct {
		slug, field, plaintext string
	}{
		{"discord", "url", "https://discord.com/api/webhooks/1/abc"},
		{"slack", "url", "https://hooks.slack.com/services/T/B/xxx"},
		{"ntfy", "secret", "ntfy-token"},
		{"webhook", "secret", "hmac-secret"},
	}
	for _, tc := range cases {
		cfg := rawConfigFor(t, app, tc.slug)
		stored, _ := cfg[tc.field].(string)
		if stored == tc.plaintext {
			t.Fatalf("%s.%s was not encrypted by the migration — still plaintext", tc.slug, tc.field)
		}
		decrypted, err := crypto.Decrypt(stored, secretKey)
		if err != nil {
			t.Fatalf("%s.%s did not decrypt after migration: %v", tc.slug, tc.field, err)
		}
		if string(decrypted) != tc.plaintext {
			t.Fatalf("%s.%s decrypted to %q, want %q", tc.slug, tc.field, decrypted, tc.plaintext)
		}
	}
}

// TestEncryptNotifierIntegrationSecretsSkipsMissingRows confirms slugs with
// no stored row yet (never configured) are a no-op, not an error.
func TestEncryptNotifierIntegrationSecretsSkipsMissingRows(t *testing.T) {
	app := newNotifierSecretsTestApp(t)

	if err := encryptNotifierIntegrationSecrets(app, []byte("cccccccccccccccccccccccccccccc32")); err != nil {
		t.Fatalf("expected no error with no integration rows at all: %v", err)
	}
}

// TestEncryptNotifierIntegrationSecretsSkipsOnInvalidSecretKey confirms the
// migration doesn't attempt to encrypt (and fail hard on boot) when
// SECRET_KEY isn't set/valid at migration time — it logs and defers, rather
// than blocking startup.
func TestEncryptNotifierIntegrationSecretsSkipsOnInvalidSecretKey(t *testing.T) {
	app := newNotifierSecretsTestApp(t)
	seedIntegrationRow(t, app, "webhook", map[string]any{"secret": "hmac-secret"})

	if err := encryptNotifierIntegrationSecrets(app, nil); err != nil {
		t.Fatalf("expected no error when secretKey is nil, got: %v", err)
	}

	cfg := rawConfigFor(t, app, "webhook")
	if cfg["secret"] != "hmac-secret" {
		t.Fatalf("expected webhook.secret to remain untouched when SECRET_KEY is invalid, got %v", cfg["secret"])
	}
}
