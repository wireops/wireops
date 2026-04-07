package secrets

import (
	"context"
	"errors"
	"fmt"

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
// Returns an error if the key is absent/invalid or if decryption fails, so
// the caller can fail-fast rather than deploying raw ciphertext or an empty value.
func (p *InternalSecretProvider) Resolve(_ context.Context, rawValue string) (string, error) {
	if len(p.secretKey) != 32 {
		return "", errors.New("internal secret provider: SECRET_KEY is not configured or is not 32 bytes")
	}
	decrypted, err := crypto.Decrypt(rawValue, p.secretKey)
	if err != nil {
		return "", fmt.Errorf("internal secret provider: failed to decrypt value: %w", err)
	}
	return string(decrypted), nil
}
