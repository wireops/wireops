package secrets

import (
	"context"

	"github.com/wireops/wireops/internal/crypto"
)

// InternalSecretProvider resolves secrets encrypted with AES-GCM using the
// application's SECRET_KEY environment variable. This is the default built-in
// provider ("internal") and matches the existing encryption behavior.
type InternalSecretProvider struct {
	secretKey []byte
}

// NewInternalProvider creates an InternalSecretProvider using the given 32-byte key.
func NewInternalProvider(secretKey []byte) *InternalSecretProvider {
	return &InternalSecretProvider{secretKey: secretKey}
}

// Name implements SecretProvider.
func (p *InternalSecretProvider) Name() string { return "internal" }

// Resolve decrypts the AES-GCM ciphertext using the application secret key.
// If the key is missing or decryption fails, the raw value is returned as-is
// so plaintext env vars still work without encryption.
func (p *InternalSecretProvider) Resolve(_ context.Context, rawValue string) (string, error) {
	if len(p.secretKey) != 32 {
		// No key configured — treat value as already plaintext.
		return rawValue, nil
	}
	decrypted, err := crypto.Decrypt(rawValue, p.secretKey)
	if err != nil {
		// Not a valid ciphertext — return as plaintext (backward-compatible).
		return rawValue, nil
	}
	return string(decrypted), nil
}
