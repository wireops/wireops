package integrations

import (
	"sync"
)

var (
	registry = make(map[string]Integration)
	mu       sync.RWMutex
)

// Register registers a new integration into the global registry.
// Panics if an integration with the same slug is already registered.
func Register(integration Integration) {
	mu.Lock()
	defer mu.Unlock()

	slug := integration.Slug()
	if _, exists := registry[slug]; exists {
		panic("integration " + slug + " already registered")
	}
	registry[slug] = integration
}

// Get returns the integration by its slug, or false if not found.
func Get(slug string) (Integration, bool) {
	mu.RLock()
	defer mu.RUnlock()

	i, ok := registry[slug]
	return i, ok
}

// All returns a slice of all registered integrations.
func All() []Integration {
	mu.RLock()
	defer mu.RUnlock()

	var all []Integration
	for _, i := range registry {
		all = append(all, i)
	}
	return all
}
