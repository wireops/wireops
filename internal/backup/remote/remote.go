// Package remote defines a provider-agnostic storage abstraction for
// off-host backup mirroring. "s3" is the only implementation today, but the
// Storage interface and factory registry are shaped so Azure Blob / GCS
// support later is an additive registration, not a schema or call-site
// rework — the same pattern internal/secrets uses for Vault/Infisical.
package remote

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// Info describes one stored backup object, mirroring internal/backup.Info
// closely enough that service.go can convert between them directly.
type Info struct {
	Key      string
	Size     int64
	Modified time.Time
}

// Storage is the provider-agnostic remote backup storage contract. Every
// method takes a bare object key (no prefix) — implementations own joining
// their own configured prefix onto it.
type Storage interface {
	// Put uploads r (size bytes) under key, with optional metadata (used by
	// the KMS envelope-encryption path to carry the wrapped data key
	// alongside the object).
	Put(ctx context.Context, key string, r io.Reader, size int64, meta map[string]string) error

	// Get returns a reader for key's content plus whatever metadata was
	// stored alongside it. Callers must Close the reader.
	Get(ctx context.Context, key string) (io.ReadCloser, map[string]string, error)

	// List returns every object under the configured prefix, most recent
	// modification first is NOT guaranteed — callers sort as needed.
	List(ctx context.Context) ([]Info, error)

	// Delete removes key. Deleting a key that doesn't exist is not an error.
	Delete(ctx context.Context, key string) error

	// EnsurePrefix makes the configured prefix visible in the backend even
	// before any real object has been written under it (some S3-compatible
	// UIs and `List` calls only "discover" a prefix once at least one
	// object — including a marker — exists there).
	EnsurePrefix(ctx context.Context) error

	Close() error
}

// KeyManager wraps/unwraps a per-backup data encryption key via an external
// key management service (KMS). It's the optional upgrade over the default
// SECRET_KEY-derived content encryption — see encrypt.go.
type KeyManager interface {
	// GenerateDataKey returns a fresh plaintext data key plus that same key
	// wrapped ("encrypted") by the KMS. Only the wrapped form is persisted;
	// the plaintext form is used once, in memory, to encrypt one backup.
	GenerateDataKey(ctx context.Context) (plaintext []byte, encrypted []byte, err error)

	// Decrypt unwraps a previously-generated data key.
	Decrypt(ctx context.Context, encrypted []byte) ([]byte, error)
}

// factory is the shape shared by both Storage and KeyManager providers:
// build an instance from a record's decrypted, provider-shaped config and
// credentials maps (see internal/backup/service.go for how those are read
// out of the backup_remote_settings collection).
type factory[T any] func(config map[string]any, credentials map[string]any) (T, error)

// registry is a tiny named-factory lookup, generic so Storage and KeyManager
// providers don't need two hand-written copies of the same registration
// boilerplate (mirrors the pattern in internal/secrets.Registry).
type registry[T any] struct {
	mu    sync.Mutex
	items map[string]factory[T]
}

func newRegistry[T any]() *registry[T] {
	return &registry[T]{items: map[string]factory[T]{}}
}

func (r *registry[T]) register(name string, f factory[T]) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[name]; exists {
		panic(fmt.Sprintf("remote: provider %q already registered", name))
	}
	r.items[name] = f
}

func (r *registry[T]) build(name string, config, credentials map[string]any) (T, error) {
	r.mu.Lock()
	f, ok := r.items[name]
	r.mu.Unlock()
	if !ok {
		var zero T
		return zero, fmt.Errorf("remote: unknown provider %q", name)
	}
	return f(config, credentials)
}

// StorageFactory builds a Storage from a record's decrypted, provider-shaped
// config and credentials maps.
type StorageFactory = factory[Storage]

// KeyManagerFactory builds a KeyManager the same way.
type KeyManagerFactory = factory[KeyManager]

var (
	storageProviders    = newRegistry[Storage]()
	keyManagerProviders = newRegistry[KeyManager]()
)

// Register adds a Storage provider factory. Panics on duplicate
// registration — providers are only ever registered once, at package init
// time.
func Register(provider string, f StorageFactory) {
	storageProviders.register(provider, f)
}

// New builds a Storage for the given provider name.
func New(provider string, config map[string]any, credentials map[string]any) (Storage, error) {
	return storageProviders.build(provider, config, credentials)
}

// RegisterKMS adds a KeyManager provider factory.
func RegisterKMS(provider string, f KeyManagerFactory) {
	keyManagerProviders.register(provider, f)
}

// NewKMS builds a KeyManager for the given provider name.
func NewKMS(provider string, config map[string]any, credentials map[string]any) (KeyManager, error) {
	return keyManagerProviders.build(provider, config, credentials)
}
