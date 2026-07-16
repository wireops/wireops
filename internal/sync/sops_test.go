package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	sopscore "github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	sopsage "github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/cmd/sops/common"
	"github.com/getsops/sops/v3/cmd/sops/formats"
	sopsconfig "github.com/getsops/sops/v3/config"
	"github.com/getsops/sops/v3/keyservice"
	"github.com/getsops/sops/v3/version"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/secrets"
)

const sopsTestSecretKey = "12345678901234567890123456789012"

// encryptForAgeTest is a copy of internal/secrets's test-only fixture
// builder: sops-wrapper's Encrypt only wires up aws/gcp/azure/vault key
// groups, not age, so building an age-encrypted fixture means driving
// getsops/sops/v3 directly with an age.MasterKey.
func encryptForAgeTest(t *testing.T, publicKey string, plaintext []byte) []byte {
	t.Helper()

	store := common.StoreForFormat(formats.Yaml, sopsconfig.NewStoresConfig())
	branches, err := store.LoadPlainFile(plaintext)
	if err != nil {
		t.Fatalf("LoadPlainFile: %v", err)
	}

	masterKey, err := sopsage.MasterKeyFromRecipient(publicKey)
	if err != nil {
		t.Fatalf("MasterKeyFromRecipient: %v", err)
	}

	tree := sopscore.Tree{
		Branches: branches,
		Metadata: sopscore.Metadata{
			KeyGroups: []sopscore.KeyGroup{{masterKey}},
			Version:   version.Version,
		},
	}

	dataKey, errs := tree.GenerateDataKeyWithKeyServices([]keyservice.KeyServiceClient{keyservice.NewLocalClient()})
	if len(errs) > 0 {
		t.Fatalf("GenerateDataKeyWithKeyServices: %v", errs)
	}
	if err := common.EncryptTree(common.EncryptTreeOpts{DataKey: dataKey, Tree: &tree, Cipher: aes.NewCipher()}); err != nil {
		t.Fatalf("EncryptTree: %v", err)
	}
	encBytes, err := store.EmitEncryptedFile(tree)
	if err != nil {
		t.Fatalf("EmitEncryptedFile: %v", err)
	}
	return encBytes
}

func newSopsTestRepo(t *testing.T, app core.App, ageKeyEncrypted, ageKeyPublic string) *core.Record {
	t.Helper()
	col := core.NewBaseCollection("repositories")
	col.Fields.Add(&core.TextField{Name: "name"})
	col.Fields.Add(&core.TextField{Name: "sops_age_key", Hidden: true})
	col.Fields.Add(&core.TextField{Name: "sops_age_public_key"})
	if err := app.Save(col); err != nil {
		t.Fatalf("failed to create repositories collection: %v", err)
	}

	rec := core.NewRecord(col)
	rec.Set("name", "test-repo")
	rec.Set("sops_age_key", ageKeyEncrypted)
	rec.Set("sops_age_public_key", ageKeyPublic)
	if err := app.Save(rec); err != nil {
		t.Fatalf("failed to create repo record: %v", err)
	}
	return rec
}

func TestLoadSopsEnvNoRepo(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	r := &Reconciler{app: app}
	values, err := r.loadSopsEnv(context.Background(), nil, t.TempDir())
	if err != nil {
		t.Fatalf("loadSopsEnv: %v", err)
	}
	if values != nil {
		t.Errorf("expected nil values for nil repo, got %#v", values)
	}
}

func TestLoadSopsEnvNoSecretsFile(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	repo := newSopsTestRepo(t, app, "", "")
	r := &Reconciler{app: app}
	values, err := r.loadSopsEnv(context.Background(), repo, t.TempDir())
	if err != nil {
		t.Fatalf("loadSopsEnv: %v", err)
	}
	if values != nil {
		t.Errorf("expected nil values when no secrets.yaml present, got %#v", values)
	}
}

func TestLoadSopsEnvMissingAgeKey(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	repo := newSopsTestRepo(t, app, "", "")
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "secrets.yaml"), []byte("whatever"), 0o644); err != nil {
		t.Fatalf("write secrets.yaml: %v", err)
	}

	r := &Reconciler{app: app}
	if _, err := r.loadSopsEnv(context.Background(), repo, workDir); err == nil {
		t.Fatal("expected error when secrets.yaml is present but repository has no age key, got nil")
	}
}

func TestLoadSopsEnvDecryptsFile(t *testing.T) {
	t.Setenv("SECRET_KEY", sopsTestSecretKey)

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	privateKey, publicKey, err := secrets.GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair: %v", err)
	}
	encryptedKey, err := crypto.Encrypt([]byte(privateKey), []byte(sopsTestSecretKey))
	if err != nil {
		t.Fatalf("crypto.Encrypt: %v", err)
	}

	repo := newSopsTestRepo(t, app, encryptedKey, publicKey)

	workDir := t.TempDir()
	encrypted := encryptForAgeTest(t, publicKey, []byte("DB_PASS: hunter2\n"))
	if err := os.WriteFile(filepath.Join(workDir, "secrets.yaml"), encrypted, 0o644); err != nil {
		t.Fatalf("write secrets.yaml: %v", err)
	}

	r := &Reconciler{app: app}
	values, err := r.loadSopsEnv(context.Background(), repo, workDir)
	if err != nil {
		t.Fatalf("loadSopsEnv: %v", err)
	}
	if values["DB_PASS"] != "hunter2" {
		t.Errorf("expected DB_PASS=hunter2, got %#v", values)
	}
}

func TestOverlaySopsEnv(t *testing.T) {
	base := []string{"A=1", "B=2"}
	overlay := map[string]string{"B": "override", "C": "3"}

	got := overlaySopsEnv(base, overlay)
	want := []string{"A=1", "B=override", "C=3"}

	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

func TestOverlaySopsEnvNoOverlayReturnsInput(t *testing.T) {
	base := []string{"A=1"}
	got := overlaySopsEnv(base, nil)
	if len(got) != 1 || got[0] != "A=1" {
		t.Errorf("expected unchanged envVars, got %v", got)
	}
}
