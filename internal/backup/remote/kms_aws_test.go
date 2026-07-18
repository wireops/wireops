package remote

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/kms"
)

type fakeKMS struct {
	dataKey       []byte
	wrappedKey    []byte
	decryptCalled []byte
}

func (f *fakeKMS) GenerateDataKey(ctx context.Context, in *kms.GenerateDataKeyInput, opts ...func(*kms.Options)) (*kms.GenerateDataKeyOutput, error) {
	return &kms.GenerateDataKeyOutput{Plaintext: f.dataKey, CiphertextBlob: f.wrappedKey}, nil
}

func (f *fakeKMS) Decrypt(ctx context.Context, in *kms.DecryptInput, opts ...func(*kms.Options)) (*kms.DecryptOutput, error) {
	f.decryptCalled = in.CiphertextBlob
	return &kms.DecryptOutput{Plaintext: f.dataKey}, nil
}

func TestAWSKeyManagerGenerateAndDecrypt(t *testing.T) {
	fake := &fakeKMS{dataKey: []byte("01234567890123456789012345678901"), wrappedKey: []byte("wrapped")}
	km := &awsKeyManager{api: fake, keyID: "alias/wireops"}

	plaintext, wrapped, err := km.GenerateDataKey(context.Background())
	if err != nil {
		t.Fatalf("GenerateDataKey failed: %v", err)
	}
	if string(plaintext) != string(fake.dataKey) {
		t.Fatalf("expected plaintext %q, got %q", fake.dataKey, plaintext)
	}
	if string(wrapped) != string(fake.wrappedKey) {
		t.Fatalf("expected wrapped key %q, got %q", fake.wrappedKey, wrapped)
	}

	decrypted, err := km.Decrypt(context.Background(), wrapped)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if string(fake.decryptCalled) != string(wrapped) {
		t.Fatalf("expected Decrypt to be called with the wrapped key, got %q", fake.decryptCalled)
	}
	if string(decrypted) != string(fake.dataKey) {
		t.Fatalf("expected decrypted %q, got %q", fake.dataKey, decrypted)
	}
}

func TestNewAWSKeyManagerRequiresConfig(t *testing.T) {
	if _, err := newAWSKeyManager(map[string]any{}, map[string]any{}); err == nil {
		t.Fatal("expected error for empty config/credentials")
	}

	km, err := newAWSKeyManager(
		map[string]any{"kms_key_id": "alias/wireops", "region": "us-east-1"},
		map[string]any{"access_key": "ak", "secret_key": "sk"},
	)
	if err != nil {
		t.Fatalf("expected valid config to succeed: %v", err)
	}
	if km == nil {
		t.Fatal("expected non-nil key manager")
	}
}

func TestNewAWSKeyManagerFallsBackToRegion(t *testing.T) {
	km, err := newAWSKeyManager(
		map[string]any{"kms_key_id": "alias/wireops", "region": "us-west-2"},
		map[string]any{"access_key": "ak", "secret_key": "sk"},
	)
	if err != nil {
		t.Fatalf("expected kms_region to fall back to region: %v", err)
	}
	if km == nil {
		t.Fatal("expected non-nil key manager")
	}
}
