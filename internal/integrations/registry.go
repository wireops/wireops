package integrations

import (
	"fmt"
	"reflect"
	"sort"
	"sync"
)

// Entry is one registered integration: its static Descriptor plus whatever
// implementation value (if any) was registered alongside it. Impl is typed
// as any because different integrations back different capability
// interfaces (or none at all, for pure-config stubs like discord/vault) —
// use GetImpl[T] or Capable[T] to retrieve it as a concrete interface type.
type Entry struct {
	Descriptor Descriptor
	Impl       any
}

var (
	registry = make(map[string]Entry)
	mu       sync.RWMutex
)

// knownCapabilityInterfaces maps a CapabilityID to the Go interface type
// Register should verify impl against, for capabilities that have a real
// interface defined. Capabilities not listed here (e.g. CapNotifier today)
// are accepted as declared intent with no implementation check — they exist
// so descriptors can express what an integration does ahead of their
// interface landing in a later phase.
var knownCapabilityInterfaces = map[CapabilityID]reflect.Type{
	CapActionProvider: reflect.TypeOf((*ActionProvider)(nil)).Elem(),
}

// Register registers a new integration into the global registry. Panics if:
//   - an integration with the same slug is already registered (same as
//     before this refactor);
//   - d declares a Capability that has a known backing interface, but impl
//     does not implement it;
//   - impl implements a known capability interface that d did not declare —
//     catches a descriptor that forgot to list a capability its impl
//     actually has.
func Register(d Descriptor, impl any) {
	mu.Lock()
	defer mu.Unlock()

	if _, exists := registry[d.Slug]; exists {
		panic("integration " + d.Slug + " already registered")
	}

	if err := validateCapabilities(d, impl); err != nil {
		panic(err.Error())
	}

	registry[d.Slug] = Entry{Descriptor: d, Impl: impl}
}

// validateCapabilities checks d.Capabilities against knownCapabilityInterfaces
// for impl, returning an error (never panicking itself) describing the first
// mismatch found — either a declared capability impl doesn't satisfy, or an
// interface impl satisfies that d didn't declare. Register panics on this;
// registry_capability_test.go calls it directly so both share one copy of
// the consistency rule instead of two independently-maintained ones.
func validateCapabilities(d Descriptor, impl any) error {
	declared := make(map[CapabilityID]bool, len(d.Capabilities))
	for _, capID := range d.Capabilities {
		declared[capID] = true
	}

	for capID, ifaceType := range knownCapabilityInterfaces {
		implementsIface := impl != nil && reflect.TypeOf(impl).Implements(ifaceType)
		switch {
		case declared[capID] && !implementsIface:
			return fmt.Errorf("integration %s declares capability %s but its implementation does not satisfy the corresponding interface", d.Slug, capID)
		case !declared[capID] && implementsIface:
			return fmt.Errorf("integration %s implementation satisfies capability %s but does not declare it", d.Slug, capID)
		}
	}
	return nil
}

// Get returns the registry Entry for slug, or false if not found.
func Get(slug string) (Entry, bool) {
	mu.RLock()
	defer mu.RUnlock()

	e, ok := registry[slug]
	return e, ok
}

// All returns every registered Entry, sorted by Category.Order then Slug —
// deterministic, unlike the old map-iteration order this replaces.
func All() []Entry {
	mu.RLock()
	defer mu.RUnlock()

	all := make([]Entry, 0, len(registry))
	for _, e := range registry {
		all = append(all, e)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Descriptor.Category.Order != all[j].Descriptor.Category.Order {
			return all[i].Descriptor.Category.Order < all[j].Descriptor.Category.Order
		}
		return all[i].Descriptor.Slug < all[j].Descriptor.Slug
	})
	return all
}

// Capable returns the Impl of every registered entry whose Impl satisfies T,
// in the same deterministic order as All().
func Capable[T any]() []T {
	var out []T
	for _, e := range All() {
		if impl, ok := e.Impl.(T); ok {
			out = append(out, impl)
		}
	}
	return out
}

// GetImpl returns slug's Impl asserted to T, and whether the slug is
// registered and its Impl satisfies T.
func GetImpl[T any](slug string) (T, bool) {
	var zero T
	e, ok := Get(slug)
	if !ok {
		return zero, false
	}
	impl, ok := e.Impl.(T)
	if !ok {
		return zero, false
	}
	return impl, true
}
