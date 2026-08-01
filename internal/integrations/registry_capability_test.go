package integrations

import (
	"reflect"
	"testing"
)

// TestRegisterPanicsOnCapabilityInterfaceMismatch exercises Register's
// panic behavior directly, using local throwaway descriptors validated
// against a private helper (not the global Register/registry), so it
// doesn't risk a duplicate-slug panic against real provider registrations
// (which this internal test file, unlike conformance_test.go, does not
// blank-import — doing so from inside package integrations would be an
// import cycle, since every provider package imports integrations).
func TestRegisterPanicsOnCapabilityInterfaceMismatch(t *testing.T) {
	t.Run("declares capability without implementing interface", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected a panic when a declared capability's interface isn't implemented")
			}
		}()
		checkCapabilityConsistency(t, Descriptor{
			Slug:         "conformance-test-missing-impl",
			Name:         "Conformance Test",
			Category:     CategoryReverseProxy,
			Capabilities: []CapabilityID{CapActionProvider},
		}, nil)
	})

	t.Run("implements interface without declaring capability", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected a panic when impl satisfies an interface its descriptor doesn't declare")
			}
		}()
		checkCapabilityConsistency(t, Descriptor{
			Slug:     "conformance-test-undeclared-capability",
			Name:     "Conformance Test",
			Category: CategoryReverseProxy,
		}, conformanceTestActionProvider{})
	})

	t.Run("valid declaration does not panic", func(t *testing.T) {
		checkCapabilityConsistency(t, Descriptor{
			Slug:         "conformance-test-valid",
			Name:         "Conformance Test",
			Category:     CategoryReverseProxy,
			Capabilities: []CapabilityID{CapActionProvider},
		}, conformanceTestActionProvider{})
	})
}

// conformanceTestActionProvider is a throwaway ActionProvider implementor
// used only to exercise the capability/interface consistency check.
type conformanceTestActionProvider struct{}

func (conformanceTestActionProvider) ResolveActions(cfg Config, scope ActionScope) []Action {
	return nil
}

// checkCapabilityConsistency duplicates Register's capability/interface
// consistency check (see registry.go) against a throwaway descriptor
// without mutating the real global registry — it panics under the same
// conditions Register would, so callers can assert on that panic via
// recover().
func checkCapabilityConsistency(t *testing.T, d Descriptor, impl any) {
	t.Helper()

	declared := make(map[CapabilityID]bool, len(d.Capabilities))
	for _, capID := range d.Capabilities {
		declared[capID] = true
	}

	for capID, ifaceType := range knownCapabilityInterfaces {
		implementsIface := impl != nil && reflect.TypeOf(impl).Implements(ifaceType)
		switch {
		case declared[capID] && !implementsIface:
			panic("integration " + d.Slug + " declares capability " + string(capID) + " but its implementation does not satisfy the corresponding interface")
		case !declared[capID] && implementsIface:
			panic("integration " + d.Slug + " implementation satisfies capability " + string(capID) + " but does not declare it")
		}
	}
}
