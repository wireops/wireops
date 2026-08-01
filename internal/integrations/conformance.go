package integrations

import "fmt"

// ValidateDescriptor checks the invariants every registered Descriptor must
// hold, independent of the global registry (so it's usable both by
// conformance_test.go against the real registry, and by tests that want to
// check a throwaway descriptor without registering it). It does not check
// capability/interface consistency — that's enforced by Register itself at
// registration time, since it needs the paired Impl value. Exported because
// the registry-wide conformance test lives in package integrations_test (to
// blank-import provider packages without an import cycle) and needs to call
// this from outside the package.
func ValidateDescriptor(d Descriptor) error {
	if d.Slug == "" {
		return fmt.Errorf("descriptor has empty Slug")
	}
	if d.Name == "" {
		return fmt.Errorf("descriptor %q has empty Name", d.Slug)
	}
	if !isKnownCategory(d.Category) {
		return fmt.Errorf("descriptor %q has unknown Category %+v", d.Slug, d.Category)
	}
	for _, f := range d.Fields {
		if f.Encrypted && !f.Sensitive {
			return fmt.Errorf("descriptor %q field %q is Encrypted but not Sensitive", d.Slug, f.Key)
		}
	}
	if len(d.Capabilities) == 0 && len(d.Fields) != 0 {
		return fmt.Errorf("descriptor %q has zero Capabilities but non-zero Fields", d.Slug)
	}
	return nil
}

// isKnownCategory reports whether c matches one of the 6 package-level
// Category vars exactly (ID, Label, and Order).
func isKnownCategory(c Category) bool {
	for _, known := range []Category{
		CategoryReverseProxy,
		CategoryLogging,
		CategoryNotification,
		CategorySecretBackend,
		CategoryStorageBackend,
		CategorySourceControl,
	} {
		if c == known {
			return true
		}
	}
	return false
}
