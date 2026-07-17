package secrets

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/wireops/wireops/internal/testutil"
)


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

	encrypted := testutil.EncryptForAge(t, publicKey, []byte("DB_PASS: hunter2\nAPI_TOKEN: \"abc123\"\n"))

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

	encrypted := testutil.EncryptForAge(t, publicKey, []byte("DB_PASS: hunter2\n"))

	if _, err := DecryptSecretsFile(context.Background(), encrypted, otherPrivateKey); err == nil {
		t.Fatal("expected error decrypting with the wrong age key, got nil")
	}
}

func TestDecryptSecretsFileMalformed(t *testing.T) {
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

	encryptedA := testutil.EncryptForAge(t, publicKeyA, []byte("WHO: alice\n"))
	encryptedB := testutil.EncryptForAge(t, publicKeyB, []byte("WHO: bob\n"))

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

func TestFindSecretsFileNoneFound(t *testing.T) {
	dir := t.TempDir()
	path, err := FindSecretsFile(dir)
	if err != nil {
		t.Fatalf("FindSecretsFile: %v", err)
	}
	if path != "" {
		t.Fatalf("path = %q, want empty", path)
	}
}

func TestFindSecretsFileFindsRegularFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "secrets.yaml")
	if err := os.WriteFile(target, []byte("DB_PASS: ENC[...]\n"), 0o644); err != nil {
		t.Fatalf("write secrets.yaml: %v", err)
	}

	path, err := FindSecretsFile(dir)
	if err != nil {
		t.Fatalf("FindSecretsFile: %v", err)
	}
	if path != target {
		t.Fatalf("path = %q, want %q", path, target)
	}
}

// TestFindSecretsFileRejectsSymlink guards against a malicious git repo
// planting "secrets.yaml" as a symlink to an arbitrary host path (e.g.
// /etc/shadow) — the repo tree checked out from a configured git_url is
// untrusted content, so a symlinked secrets file must be rejected rather
// than transparently followed and read.
func TestFindSecretsFileRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	outsideTarget := filepath.Join(t.TempDir(), "host-secret.txt")
	if err := os.WriteFile(outsideTarget, []byte("super-secret-host-file"), 0o644); err != nil {
		t.Fatalf("write outside target: %v", err)
	}
	linkPath := filepath.Join(dir, "secrets.yaml")
	if err := os.Symlink(outsideTarget, linkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	path, err := FindSecretsFile(dir)
	if err == nil {
		t.Fatalf("expected FindSecretsFile to reject a symlinked secrets file, got path=%q", path)
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error = %q, want it to mention symlink rejection", err)
	}
}
