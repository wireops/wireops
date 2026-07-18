package backup

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/wireops/wireops/internal/crypto"
)

const testS3SecretKey = "01234567890123456789012345678901" // 32 bytes

// newS3TestApp returns a test PocketBase app with a minimal integrations
// collection, matching pb_migrations/01_init_collections.go's
// createIntegrations (tests.NewTestApp doesn't run wireops's own
// migrations — see internal/secrets/test_helpers_test.go for the same
// pattern used there).
func newS3TestApp(t *testing.T) core.App {
	t.Helper()
	t.Setenv("SECRET_KEY", testS3SecretKey)
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(app.Cleanup)

	col := core.NewBaseCollection("integrations")
	col.Fields.Add(&core.TextField{Name: "slug", Required: true})
	col.Fields.Add(&core.BoolField{Name: "enabled"})
	col.Fields.Add(&core.JSONField{Name: "config"})
	if err := app.Save(col); err != nil {
		t.Fatalf("save integrations collection: %v", err)
	}

	return app
}

func saveS3Integration(t *testing.T, app core.App, enabled bool, config map[string]any) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("integrations")
	if err != nil {
		t.Fatalf("find integrations collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("slug", "s3")
	rec.Set("enabled", enabled)
	rec.Set("config", config)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save s3 integration: %v", err)
	}
}

func TestRemoteEnabledFalseWithoutIntegrationRow(t *testing.T) {
	app := newS3TestApp(t)
	enabled, err := remoteEnabled(app)
	if err != nil {
		t.Fatalf("remoteEnabled failed: %v", err)
	}
	if enabled {
		t.Fatal("expected remote storage to be disabled with no s3 integration row")
	}
}

func TestRemoteEnabledFalseWhenDisabled(t *testing.T) {
	app := newS3TestApp(t)
	encryptedSecret, err := crypto.Encrypt([]byte("s3cr3t"), crypto.NormalizeSecretKey(testS3SecretKey))
	if err != nil {
		t.Fatalf("encrypt secret: %v", err)
	}
	saveS3Integration(t, app, false, map[string]any{
		"bucket": "b", "region": "r", "access_key": "ak", "secret": encryptedSecret,
	})

	enabled, err := remoteEnabled(app)
	if err != nil {
		t.Fatalf("remoteEnabled failed: %v", err)
	}
	if enabled {
		t.Fatal("expected remote storage to be disabled when the s3 integration row has enabled=false")
	}
}

func TestS3IntegrationConfigDecryptsSecret(t *testing.T) {
	app := newS3TestApp(t)
	encryptedSecret, err := crypto.Encrypt([]byte("s3cr3t"), crypto.NormalizeSecretKey(testS3SecretKey))
	if err != nil {
		t.Fatalf("encrypt secret: %v", err)
	}
	saveS3Integration(t, app, true, map[string]any{
		"bucket": "my-bucket", "region": "us-east-1", "access_key": "ak", "secret": encryptedSecret,
	})

	config, ok, err := s3IntegrationConfig(app)
	if err != nil {
		t.Fatalf("s3IntegrationConfig failed: %v", err)
	}
	if !ok {
		t.Fatal("expected s3 integration to be enabled")
	}
	if config["secret"] != "s3cr3t" {
		t.Fatalf("expected decrypted secret %q, got %q", "s3cr3t", config["secret"])
	}
	if config["bucket"] != "my-bucket" {
		t.Fatalf("expected bucket to pass through untouched, got %q", config["bucket"])
	}

	enabled, err := remoteEnabled(app)
	if err != nil {
		t.Fatalf("remoteEnabled failed: %v", err)
	}
	if !enabled {
		t.Fatal("expected remote storage to be enabled")
	}
}

func TestRemoteEncryptContentDefaultsTrueWithoutConfig(t *testing.T) {
	app := newS3TestApp(t)
	if !remoteEncryptContent(app) {
		t.Fatal("expected remoteEncryptContent to default to true with no s3 integration row")
	}
}

func TestRemoteEncryptContentRespectsConfigFlag(t *testing.T) {
	app := newS3TestApp(t)
	encryptedSecret, err := crypto.Encrypt([]byte("s3cr3t"), crypto.NormalizeSecretKey(testS3SecretKey))
	if err != nil {
		t.Fatalf("encrypt secret: %v", err)
	}
	saveS3Integration(t, app, true, map[string]any{
		"bucket": "b", "region": "r", "access_key": "ak", "secret": encryptedSecret, "encrypt_content": false,
	})

	if remoteEncryptContent(app) {
		t.Fatal("expected remoteEncryptContent to be false when explicitly disabled")
	}
}

func TestMigrateLegacyS3SettingsNoopWithoutLegacyConfig(t *testing.T) {
	app := newS3TestApp(t)
	if err := MigrateLegacyS3Settings(app, crypto.NormalizeSecretKey(testS3SecretKey)); err != nil {
		t.Fatalf("MigrateLegacyS3Settings failed: %v", err)
	}
	if _, err := app.FindFirstRecordByFilter("integrations", "slug = {:slug}", map[string]any{"slug": "s3"}); err == nil {
		t.Fatal("expected no s3 integration row to be created when PocketBase's native S3 backend was never enabled")
	}
}

func TestMigrateLegacyS3SettingsCarriesOverAndDisablesNativeS3(t *testing.T) {
	app := newS3TestApp(t)
	settings := app.Settings()
	settings.Backups.S3.Enabled = true
	settings.Backups.S3.Bucket = "legacy-bucket"
	settings.Backups.S3.Region = "us-east-1"
	settings.Backups.S3.Endpoint = "https://s3.example.com"
	settings.Backups.S3.AccessKey = "legacy-ak"
	settings.Backups.S3.Secret = "legacy-secret"
	if err := app.Save(settings); err != nil {
		t.Fatalf("save legacy S3 settings: %v", err)
	}

	if err := MigrateLegacyS3Settings(app, crypto.NormalizeSecretKey(testS3SecretKey)); err != nil {
		t.Fatalf("MigrateLegacyS3Settings failed: %v", err)
	}

	rec, err := app.FindFirstRecordByFilter("integrations", "slug = {:slug}", map[string]any{"slug": "s3"})
	if err != nil {
		t.Fatalf("expected s3 integration row to be created: %v", err)
	}
	if !rec.GetBool("enabled") {
		t.Fatal("expected migrated s3 integration to be enabled")
	}

	config, ok, err := s3IntegrationConfig(app)
	if err != nil || !ok {
		t.Fatalf("expected s3IntegrationConfig to read the migrated row, ok=%v err=%v", ok, err)
	}
	if config["bucket"] != "legacy-bucket" {
		t.Fatalf("expected bucket %q, got %q", "legacy-bucket", config["bucket"])
	}
	if config["secret"] != "legacy-secret" {
		t.Fatalf("expected decrypted secret %q, got %q", "legacy-secret", config["secret"])
	}

	if app.Settings().Backups.S3.Enabled {
		t.Fatal("expected PocketBase's native S3 backend to be disabled after migration")
	}

	// Idempotent: running it again must not clobber or duplicate the row.
	if err := MigrateLegacyS3Settings(app, crypto.NormalizeSecretKey(testS3SecretKey)); err != nil {
		t.Fatalf("second MigrateLegacyS3Settings call failed: %v", err)
	}
	recs, err := app.FindAllRecords("integrations")
	if err != nil {
		t.Fatalf("query integrations: %v", err)
	}
	count := 0
	for _, r := range recs {
		if r.GetString("slug") == "s3" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 s3 integration row, got %d", count)
	}
}
