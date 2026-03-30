// Package secrets defines the SecretProvider interface and a Registry
// for resolving encrypted or externally-managed secret values at deploy time.
//
// Current implementation: "wireops" (AES-GCM, local key).
// Future: "vault" and "infisical" stubs ready for implementation.
package secrets

import (
	"context"
	"fmt"
)

// SecretProvider resolves a raw stored secret value into its plaintext form.
// Each implementation handles a specific backend (wireops local key, Vault, Infisical, etc.).
type SecretProvider interface {
	// Name returns the unique identifier for this provider (e.g. "wireops", "vault").
	Name() string

	// Resolve decrypts or fetches the plaintext value of a secret.
	// rawValue is the string stored in the database for this env var.
	// For wireops, this is an AES-GCM base64-encoded ciphertext.
	// For external providers, this may be a path/reference to the secret.
	Resolve(ctx context.Context, rawValue string) (string, error)
}

// Registry holds a set of named SecretProviders.
type Registry struct {
	providers map[string]SecretProvider
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]SecretProvider)}
}

// Register adds a provider to the registry. Panics if a provider with the same
// name is already registered (programming error — providers are registered at startup).
func (r *Registry) Register(p SecretProvider) {
	if _, exists := r.providers[p.Name()]; exists {
		panic(fmt.Sprintf("secrets: provider %q already registered", p.Name()))
	}
	r.providers[p.Name()] = p
}

// Get returns the provider for the given name, or an error if unknown.
// If name is empty, it defaults to "internal".
func (r *Registry) Get(name string) (SecretProvider, error) {
	if name == "" {
		name = "internal"
	}
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("secrets: unknown provider %q", name)
	}
	return p, nil
}
