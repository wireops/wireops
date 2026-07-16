package secrets

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	sopsage "github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/cmd/sops/common"
	"github.com/getsops/sops/v3/cmd/sops/formats"
	sopsconfig "github.com/getsops/sops/v3/config"
	"github.com/getsops/sops/v3/keyservice"
	"github.com/getsops/sops/v3/version"
)

// encryptForAge builds a SOPS-encrypted YAML document for the given age
// recipient, bypassing sops-wrapper's Encrypt (which only wires up
// aws/gcp/azure/vault key groups, not age) by driving the same
// getsops/sops/v3 primitives directly with an age.MasterKey.
func encryptForAge(t *testing.T, publicKey string, plaintext []byte) []byte {
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

	tree := sops.Tree{
		Branches: branches,
		Metadata: sops.Metadata{
			KeyGroups: []sops.KeyGroup{{masterKey}},
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

func TestGenerateAgeKeypair(t *testing.T) {
	privateKey, publicKey, err := GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair: %v", err)
	}
	if !strings.HasPrefix(privateKey, "AGE-SECRET-KEY-1") {
		t.Errorf("expected private key to start with AGE-SECRET-KEY-1, got %q", privateKey)
	}
	if !strings.HasPrefix(publicKey, "age1") {
		t.Errorf("expected public key to start with age1, got %q", publicKey)
	}

	privateKey2, publicKey2, err := GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair (2nd): %v", err)
	}
	if privateKey == privateKey2 || publicKey == publicKey2 {
		t.Error("expected two calls to GenerateAgeKeypair to produce distinct keypairs")
	}
}

func TestDecryptSecretsFileSuccess(t *testing.T) {
	privateKey, publicKey, err := GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair: %v", err)
	}

	encrypted := encryptForAge(t, publicKey, []byte("DB_PASS: hunter2\nAPI_TOKEN: \"abc123\"\n"))

	values, err := DecryptSecretsFile(context.Background(), encrypted, privateKey)
	if err != nil {
		t.Fatalf("DecryptSecretsFile: %v", err)
	}
	if values["DB_PASS"] != "hunter2" || values["API_TOKEN"] != "abc123" {
		t.Errorf("unexpected decrypted values: %#v", values)
	}
}

func TestDecryptSecretsFileWrongKey(t *testing.T) {
	_, publicKey, err := GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair: %v", err)
	}
	otherPrivateKey, _, err := GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair (other): %v", err)
	}

	encrypted := encryptForAge(t, publicKey, []byte("DB_PASS: hunter2\n"))

	if _, err := DecryptSecretsFile(context.Background(), encrypted, otherPrivateKey); err == nil {
		t.Fatal("expected error decrypting with the wrong age key, got nil")
	}
}

func TestDecryptSecretsFileMalformed(t *testing.T) {
	_, _, err := GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair: %v", err)
	}
	privateKey, _, err := GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair: %v", err)
	}

	if _, err := DecryptSecretsFile(context.Background(), []byte("not a sops file"), privateKey); err == nil {
		t.Fatal("expected error decrypting malformed content, got nil")
	}
}

func TestDecryptSecretsFileNoAgeKey(t *testing.T) {
	if _, err := DecryptSecretsFile(context.Background(), []byte("whatever"), ""); err == nil {
		t.Fatal("expected error when age key is empty, got nil")
	}
}

func TestFlattenSecretsYAMLRejectsNestedValues(t *testing.T) {
	_, err := flattenSecretsYAML([]byte("DB:\n  password: hunter2\n"))
	if err == nil {
		t.Fatal("expected error for nested value, got nil")
	}
	if !strings.Contains(err.Error(), "flat map") {
		t.Errorf("expected error to mention flat map requirement, got: %v", err)
	}
}

func TestFlattenSecretsYAMLScalarTypes(t *testing.T) {
	values, err := flattenSecretsYAML([]byte("A: hello\nB: 5432\nC: true\nD: 3.5\n"))
	if err != nil {
		t.Fatalf("flattenSecretsYAML: %v", err)
	}
	want := map[string]string{"A": "hello", "B": "5432", "C": "true", "D": "3.5"}
	for k, v := range want {
		if values[k] != v {
			t.Errorf("key %s: expected %q, got %q", k, v, values[k])
		}
	}
}

// TestDecryptSecretsFileConcurrentDifferentKeys exercises DecryptSecretsFile
// under concurrency with two different repositories' age keys, guarding
// against the SOPS_AGE_KEY_FILE process-env race that decryptMu exists to
// prevent.
func TestDecryptSecretsFileConcurrentDifferentKeys(t *testing.T) {
	privateKeyA, publicKeyA, err := GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair A: %v", err)
	}
	privateKeyB, publicKeyB, err := GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair B: %v", err)
	}

	encryptedA := encryptForAge(t, publicKeyA, []byte("WHO: alice\n"))
	encryptedB := encryptForAge(t, publicKeyB, []byte("WHO: bob\n"))

	var wg sync.WaitGroup
	errs := make([]error, 20)
	results := make([]string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var content []byte
			var key string
			if i%2 == 0 {
				content, key = encryptedA, privateKeyA
			} else {
				content, key = encryptedB, privateKeyB
			}
			values, err := DecryptSecretsFile(context.Background(), content, key)
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = values["WHO"]
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
		want := "alice"
		if i%2 != 0 {
			want = "bob"
		}
		if results[i] != want {
			t.Errorf("goroutine %d: expected %q, got %q", i, want, results[i])
		}
	}
}

func TestEncryptSecretsMapRoundTrip(t *testing.T) {
	privateKey, publicKey, err := GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair: %v", err)
	}

	encrypted, err := EncryptSecretsMap(context.Background(), map[string]string{
		"DB_PASS": "hunter2",
		"API_KEY": "abc123",
	}, publicKey)
	if err != nil {
		t.Fatalf("EncryptSecretsMap: %v", err)
	}

	values, err := DecryptSecretsFile(context.Background(), encrypted, privateKey)
	if err != nil {
		t.Fatalf("DecryptSecretsFile: %v", err)
	}
	if values["DB_PASS"] != "hunter2" || values["API_KEY"] != "abc123" {
		t.Errorf("unexpected round-trip values: %#v", values)
	}
}

func TestEncryptSecretsMapRejectsInvalidKeyName(t *testing.T) {
	_, publicKey, err := GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair: %v", err)
	}

	if _, err := EncryptSecretsMap(context.Background(), map[string]string{"not a valid key": "x"}, publicKey); err == nil {
		t.Fatal("expected error for invalid key name, got nil")
	}
}

func TestEncryptSecretsMapRejectsEmptyValues(t *testing.T) {
	_, publicKey, err := GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair: %v", err)
	}

	if _, err := EncryptSecretsMap(context.Background(), map[string]string{}, publicKey); err == nil {
		t.Fatal("expected error for empty values map, got nil")
	}
}

func TestEncryptSecretsMapRejectsEmptyPublicKey(t *testing.T) {
	if _, err := EncryptSecretsMap(context.Background(), map[string]string{"A": "b"}, ""); err == nil {
		t.Fatal("expected error for empty public key, got nil")
	}
}

func TestEncryptSecretsMapRejectsInvalidPublicKey(t *testing.T) {
	if _, err := EncryptSecretsMap(context.Background(), map[string]string{"A": "b"}, "not-a-real-age-recipient"); err == nil {
		t.Fatal("expected error for invalid public key, got nil")
	}
}
