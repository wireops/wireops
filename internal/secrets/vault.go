package secrets

import (
	"context"
	"errors"
)

// VaultSecretProvider is a stub for HashiCorp Vault secret resolution.
// rawValue is expected to be a Vault secret path (e.g. "secret/data/myapp#DB_PASS").
//
// TODO: implement Vault client integration when ready.
type VaultSecretProvider struct{}

// NewVaultProvider creates a VaultSecretProvider stub.
func NewVaultProvider() *VaultSecretProvider { return &VaultSecretProvider{} }

// Name implements SecretProvider.
func (p *VaultSecretProvider) Name() string { return "vault" }

// Resolve is not yet implemented and always returns an error.
func (p *VaultSecretProvider) Resolve(_ context.Context, _ string) (string, error) {
	return "", errors.New("vault: secret provider not yet implemented")
}
