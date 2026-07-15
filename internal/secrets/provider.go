// Package secrets defines the SecretProvider interface and a Registry
// for resolving encrypted or externally-managed secret values at deploy time.
//
// Implementations: "internal" (AES-GCM, local key), "vault" (HashiCorp
// Vault KV v2), "infisical" (Infisical Universal Auth).
package secrets

import (
	"context"
	"fmt"
	"strings"
	"sync"

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

// resolveCacheKey is the context key WithResolveCache attaches a
// *resolveCache under.
type resolveCacheKey struct{}

// resolveCache caches each external provider's built connection (a Vault
// client + scoping config, or an authenticated Infisical client) for the
// lifetime of a single LoadStack/LoadJob resolution pass. Without it,
// VaultSecretProvider and InfisicalSecretProvider rebuild their connection —
// a DB read plus, for Infisical, a full Universal Auth network login — on
// every secret env var resolved, even when several secrets in the same pass
// share a backend.
type resolveCache struct {
	mu    sync.Mutex
	items map[string]any
}

// WithResolveCache returns a context carrying a fresh per-pass connection
// cache for Vault/Infisical providers. Call it once per LoadStack/LoadJob
// invocation — never store the resulting context beyond that single pass —
// so each pass re-reads the integrations collection and observes config
// changes; the cache only avoids rebuilding a connection *within* one pass.
func WithResolveCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, resolveCacheKey{}, &resolveCache{items: make(map[string]any)})
}

type cachedConn[T any] struct {
	val T
	err error
}

// loadCachedConn returns the cached value for key from ctx's resolve cache,
// calling build to populate it the first time key is requested in this pass.
// If ctx carries no resolve cache (e.g. a Resolve call outside
// LoadStack/LoadJob), build runs uncached on every call.
func loadCachedConn[T any](ctx context.Context, key string, build func() (T, error)) (T, error) {
	cache, _ := ctx.Value(resolveCacheKey{}).(*resolveCache)
	if cache == nil {
		return build()
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if v, ok := cache.items[key]; ok {
		entry := v.(cachedConn[T])
		return entry.val, entry.err
	}
	val, err := build()
	cache.items[key] = cachedConn[T]{val: val, err: err}
	return val, err
}

// ValidateReference checks that rawValue is a well-formed, non-empty locator
// for the given external provider (vault/infisical), without contacting the
// backend. It rejects blank values and values that don't match the
// provider's "<...>#<field>" reference syntax, so a plaintext secret
// accidentally pasted into the value field is caught at save time instead of
// silently persisting unencrypted and only failing at deploy-time Resolve.
func ValidateReference(provider, rawValue string) error {
	if strings.TrimSpace(rawValue) == "" {
		return fmt.Errorf("secrets: %s reference must not be empty", provider)
	}
	switch provider {
	case "vault":
		_, _, err := parseVaultReference(rawValue)
		return err
	case "infisical":
		_, _, _, _, err := parseInfisicalReference(rawValue)
		return err
	default:
		return fmt.Errorf("secrets: %q is not an external provider", provider)
	}
}
