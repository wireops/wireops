package gitprovider

import "sync"

var (
	registry = make(map[string]Provider)
	mu       sync.RWMutex
)

// Register registers a new provider into the global registry.
// Panics if a provider with the same slug is already registered.
func Register(provider Provider) {
	mu.Lock()
	defer mu.Unlock()

	slug := provider.Slug()
	if _, exists := registry[slug]; exists {
		panic("gitprovider: provider " + slug + " already registered")
	}
	registry[slug] = provider
}

// Get returns the provider by its slug, or false if not found.
func Get(slug string) (Provider, bool) {
	mu.RLock()
	defer mu.RUnlock()

	p, ok := registry[slug]
	return p, ok
}

// All returns a slice of all registered providers.
func All() []Provider {
	mu.RLock()
	defer mu.RUnlock()

	var all []Provider
	for _, p := range registry {
		all = append(all, p)
	}
	return all
}
