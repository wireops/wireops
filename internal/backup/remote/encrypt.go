package remote

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/wireops/wireops/internal/crypto"
)

// Content-encryption metadata keys, stored alongside the object so restore
// can reverse whatever encryption (if any) was applied at upload time.
// Backups written before this feature existed, or with encrypt_content
// disabled, simply have no "wireops-encryption" metadata key.
const (
	metaEncryption   = "wireops-encryption"
	metaEncryptedDEK = "wireops-encrypted-dek"
	encryptionSecret = "secret_key"
	encryptionKMS    = "kms"
)

// EncryptedPut encrypts the full contents of r (buffered in memory — see
// the streaming-vs-whole-buffer note in internal/backup/service.go) and
// uploads the ciphertext to storage under key, recording how to reverse it
// in object metadata.
//
// If km is non-nil, a fresh per-backup data key is generated via the KMS
// and the wrapped (KMS-encrypted) form of that key is stored as metadata —
// only the plaintext form, held in memory for this one call, ever touches
// the archive. If km is nil, secretKey (the SECRET_KEY-derived key) encrypts
// the archive directly.
func EncryptedPut(ctx context.Context, storage Storage, key string, r io.Reader, secretKey []byte, km KeyManager) error {
	plaintext, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("remote: read backup content: %w", err)
	}

	encKey := secretKey
	meta := map[string]string{metaEncryption: encryptionSecret}

	if km != nil {
		dek, wrappedDEK, err := km.GenerateDataKey(ctx)
		if err != nil {
			return fmt.Errorf("remote: generate data key: %w", err)
		}
		encKey = dek
		meta[metaEncryption] = encryptionKMS
		meta[metaEncryptedDEK] = base64.StdEncoding.EncodeToString(wrappedDEK)
	}

	ciphertext, err := crypto.Encrypt(plaintext, encKey)
	if err != nil {
		return fmt.Errorf("remote: encrypt backup content: %w", err)
	}

	body := []byte(ciphertext)
	return storage.Put(ctx, key, bytes.NewReader(body), int64(len(body)), meta)
}

// EncryptedGet downloads key from storage and reverses whatever encryption
// its metadata says was applied (none, SECRET_KEY, or KMS-wrapped).
func EncryptedGet(ctx context.Context, storage Storage, key string, secretKey []byte, km KeyManager) (io.ReadCloser, error) {
	body, meta, err := storage.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("remote: read backup content: %w", err)
	}

	switch meta[metaEncryption] {
	case "":
		return io.NopCloser(bytes.NewReader(raw)), nil

	case encryptionSecret:
		plaintext, err := crypto.Decrypt(string(raw), secretKey)
		if err != nil {
			return nil, fmt.Errorf("remote: decrypt backup content: %w", err)
		}
		return io.NopCloser(bytes.NewReader(plaintext)), nil

	case encryptionKMS:
		if km == nil {
			return nil, fmt.Errorf("remote: backup %q was encrypted with KMS but no KeyManager is configured", key)
		}
		wrappedDEK, err := base64.StdEncoding.DecodeString(meta[metaEncryptedDEK])
		if err != nil {
			return nil, fmt.Errorf("remote: decode wrapped data key: %w", err)
		}
		dek, err := km.Decrypt(ctx, wrappedDEK)
		if err != nil {
			return nil, fmt.Errorf("remote: unwrap data key: %w", err)
		}
		plaintext, err := crypto.Decrypt(string(raw), dek)
		if err != nil {
			return nil, fmt.Errorf("remote: decrypt backup content: %w", err)
		}
		return io.NopCloser(bytes.NewReader(plaintext)), nil

	default:
		return nil, fmt.Errorf("remote: backup %q has unknown encryption metadata %q", key, meta[metaEncryption])
	}
}
