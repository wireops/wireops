package remote

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

// fakeStorage is a minimal in-memory Storage, enough to exercise
// EncryptedPut/EncryptedGet without any real backend.
type fakeStorage struct {
	objects map[string]struct {
		body []byte
		meta map[string]string
	}
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{objects: map[string]struct {
		body []byte
		meta map[string]string
	}{}}
}

func (f *fakeStorage) Put(ctx context.Context, key string, r io.Reader, size int64, meta map[string]string) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.objects[key] = struct {
		body []byte
		meta map[string]string
	}{body: data, meta: meta}
	return nil
}

func (f *fakeStorage) Get(ctx context.Context, key string) (io.ReadCloser, map[string]string, error) {
	obj, ok := f.objects[key]
	if !ok {
		return nil, nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(obj.body)), obj.meta, nil
}

func (f *fakeStorage) List(ctx context.Context) ([]Info, error) { return nil, nil }
func (f *fakeStorage) Delete(ctx context.Context, key string) error {
	delete(f.objects, key)
	return nil
}
func (f *fakeStorage) EnsurePrefix(ctx context.Context) error { return nil }
func (f *fakeStorage) Close() error                           { return nil }

var testSecretKey = []byte("01234567890123456789012345678901") // 32 bytes

func TestEncryptedPutGetRoundTripWithSecretKey(t *testing.T) {
	storage := newFakeStorage()
	plaintext := []byte("this is a backup archive")

	if err := EncryptedPut(context.Background(), storage, "backup.zip", bytes.NewReader(plaintext), testSecretKey, nil); err != nil {
		t.Fatalf("EncryptedPut failed: %v", err)
	}

	obj := storage.objects["backup.zip"]
	if bytes.Equal(obj.body, plaintext) {
		t.Fatal("expected stored content to be encrypted, got plaintext")
	}
	if obj.meta[metaEncryption] != encryptionSecret {
		t.Fatalf("expected encryption metadata %q, got %q", encryptionSecret, obj.meta[metaEncryption])
	}

	body, err := EncryptedGet(context.Background(), storage, "backup.zip", testSecretKey, nil)
	if err != nil {
		t.Fatalf("EncryptedGet failed: %v", err)
	}
	defer body.Close()
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read decrypted body: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("expected decrypted content %q, got %q", plaintext, got)
	}
}

func TestEncryptedPutGetRoundTripWithKMS(t *testing.T) {
	storage := newFakeStorage()
	plaintext := []byte("this is a backup archive, KMS-wrapped")
	fakeKM := &fakeKeyManager{dataKey: []byte("98765432109876543210987654321098")}

	if err := EncryptedPut(context.Background(), storage, "backup.zip", bytes.NewReader(plaintext), testSecretKey, fakeKM); err != nil {
		t.Fatalf("EncryptedPut failed: %v", err)
	}

	obj := storage.objects["backup.zip"]
	if obj.meta[metaEncryption] != encryptionKMS {
		t.Fatalf("expected encryption metadata %q, got %q", encryptionKMS, obj.meta[metaEncryption])
	}
	if obj.meta[metaEncryptedDEK] == "" {
		t.Fatal("expected a wrapped data key in metadata")
	}

	body, err := EncryptedGet(context.Background(), storage, "backup.zip", testSecretKey, fakeKM)
	if err != nil {
		t.Fatalf("EncryptedGet failed: %v", err)
	}
	defer body.Close()
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read decrypted body: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("expected decrypted content %q, got %q", plaintext, got)
	}
}

func TestEncryptedGetUnencryptedObjectPassesThrough(t *testing.T) {
	storage := newFakeStorage()
	plaintext := []byte("a backup uploaded before content encryption existed")
	if err := storage.Put(context.Background(), "legacy.zip", bytes.NewReader(plaintext), int64(len(plaintext)), nil); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	body, err := EncryptedGet(context.Background(), storage, "legacy.zip", testSecretKey, nil)
	if err != nil {
		t.Fatalf("EncryptedGet failed: %v", err)
	}
	defer body.Close()
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("expected passthrough plaintext %q, got %q", plaintext, got)
	}
}

func TestEncryptedGetKMSObjectWithoutKeyManagerFails(t *testing.T) {
	storage := newFakeStorage()
	fakeKM := &fakeKeyManager{dataKey: []byte("98765432109876543210987654321098")}
	if err := EncryptedPut(context.Background(), storage, "backup.zip", bytes.NewReader([]byte("data")), testSecretKey, fakeKM); err != nil {
		t.Fatalf("EncryptedPut failed: %v", err)
	}

	if _, err := EncryptedGet(context.Background(), storage, "backup.zip", testSecretKey, nil); err == nil {
		t.Fatal("expected error decrypting a KMS-wrapped backup without a KeyManager")
	}
}

// fakeKeyManager is a minimal in-memory KeyManager for encrypt_test.go —
// GenerateDataKey returns dataKey as both plaintext and "wrapped" form
// (base64 round-trips the same bytes back on Decrypt), since these tests
// only exercise EncryptedPut/EncryptedGet's own logic, not a real KMS.
type fakeKeyManager struct {
	dataKey []byte
}

func (f *fakeKeyManager) GenerateDataKey(ctx context.Context) ([]byte, []byte, error) {
	return f.dataKey, f.dataKey, nil
}

func (f *fakeKeyManager) Decrypt(ctx context.Context, encrypted []byte) ([]byte, error) {
	return encrypted, nil
}
