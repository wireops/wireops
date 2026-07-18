package crypto

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func newCanaryTestApp(t *testing.T) core.App {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(app.Cleanup)

	col := core.NewBaseCollection("secret_key_canary")
	col.Fields.Add(&core.TextField{Name: "value", Required: true, Hidden: true})
	col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
	if err := app.Save(col); err != nil {
		t.Fatalf("failed to create secret_key_canary collection: %v", err)
	}
	return app
}

func TestVerifyOrSeedSecretKeyCanarySeedsOnFirstBoot(t *testing.T) {
	app := newCanaryTestApp(t)
	key := []byte("01234567890123456789012345678901"[:32])

	if err := VerifyOrSeedSecretKeyCanary(app, key); err != nil {
		t.Fatalf("unexpected error seeding canary: %v", err)
	}

	recs, err := app.FindAllRecords("secret_key_canary")
	if err != nil {
		t.Fatalf("failed to query canary records: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 canary record after seeding, got %d", len(recs))
	}
}

func TestVerifyOrSeedSecretKeyCanaryAcceptsMatchingKeyOnSubsequentBoot(t *testing.T) {
	app := newCanaryTestApp(t)
	key := []byte("01234567890123456789012345678901"[:32])

	if err := VerifyOrSeedSecretKeyCanary(app, key); err != nil {
		t.Fatalf("failed to seed canary: %v", err)
	}
	if err := VerifyOrSeedSecretKeyCanary(app, key); err != nil {
		t.Fatalf("expected matching key to verify successfully, got: %v", err)
	}
}

func TestVerifyOrSeedSecretKeyCanaryRejectsMismatchedKey(t *testing.T) {
	app := newCanaryTestApp(t)
	seedKey := []byte("01234567890123456789012345678901"[:32])
	wrongKey := []byte("98765432109876543210987654321098"[:32])

	if err := VerifyOrSeedSecretKeyCanary(app, seedKey); err != nil {
		t.Fatalf("failed to seed canary: %v", err)
	}

	err := VerifyOrSeedSecretKeyCanary(app, wrongKey)
	if err == nil {
		t.Fatal("expected error for mismatched SECRET_KEY, got nil")
	}
}
