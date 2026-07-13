// Package secrets defines the SecretProvider interface and a Registry
// for resolving encrypted or externally-managed secret values at deploy time.
//
// Implementations: "internal" (AES-GCM, local key), "vault" (HashiCorp
// Vault KV v2), "infisical" (Infisical Universal Auth).
package secrets

import (
	"context"
	"fmt"

	"github.com/pocketbase/pocketbase/core"
)

// ValidProviders lists the names of providers whose Resolve is fully implemented.
// Both the schema migration constraint and the hook-level validation reference this list
// to ensure that only resolvable providers can be persisted.
var ValidProviders = []string{"internal", "vault", "infisical"}

// SecretProvider resolves a raw stored secret value into its plaintext form.
// Each implementation handles a specific backend (internal local key, Vault, Infisical, etc.).
type SecretProvider interface {
	// Name returns the unique identifier for this provider (e.g. "internal", "vault").
	Name() string

	// Resolve decrypts or fetches the plaintext value of a secret.
	// rawValue is the string stored in the database for this env var.
	// For the internal provider, this is an AES-GCM base64-encoded ciphertext.
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

// NewDefaultRegistry returns the providers available to deploy/runtime
// resolution. Vault and Infisical look up their connection config from the
// integrations collection lazily (at Resolve time) rather than at
// construction, so config changes take effect without a server restart.
func NewDefaultRegistry(app core.App, secretKey []byte) *Registry {
	reg := NewRegistry()
	reg.Register(NewInternalProvider(secretKey))
	reg.Register(NewVaultProvider(app))
	reg.Register(NewInfisicalProvider(app))
	return reg
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
