package secrets

import (
	"context"
	"errors"
)

// InfisicalSecretProvider is a stub for Infisical secret resolution.
// rawValue is expected to be an Infisical secret reference
// (e.g. "project-slug/env/path#SECRET_NAME").
//
// TODO: implement Infisical client integration when ready.
type InfisicalSecretProvider struct{}

// NewInfisicalProvider creates an InfisicalSecretProvider stub.
func NewInfisicalProvider() *InfisicalSecretProvider { return &InfisicalSecretProvider{} }

// Name implements SecretProvider.
func (p *InfisicalSecretProvider) Name() string { return "infisical" }

// Resolve is not yet implemented and always returns an error.
func (p *InfisicalSecretProvider) Resolve(_ context.Context, _ string) (string, error) {
	return "", errors.New("infisical: secret provider not yet implemented")
}
