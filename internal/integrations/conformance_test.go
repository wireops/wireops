// Package integrations_test blank-imports every provider package so the
// global registry (populated by their init() registrations) is non-empty
// when this package's tests run — this can't live in an internal
// (package integrations) test file, since every provider package imports
// integrations and that would be an import cycle. This file only depends on
// integrations' exported surface (All, ValidateDescriptor).
package integrations_test

import (
	"testing"

	"github.com/wireops/wireops/internal/integrations"

	_ "github.com/wireops/wireops/internal/integrations/caddy"
	_ "github.com/wireops/wireops/internal/integrations/discord"
	_ "github.com/wireops/wireops/internal/integrations/dozzle"
	_ "github.com/wireops/wireops/internal/integrations/github"
	_ "github.com/wireops/wireops/internal/integrations/infisical"
	_ "github.com/wireops/wireops/internal/integrations/nginxproxymanager"
	_ "github.com/wireops/wireops/internal/integrations/ntfy"
	_ "github.com/wireops/wireops/internal/integrations/s3"
	_ "github.com/wireops/wireops/internal/integrations/slack"
	_ "github.com/wireops/wireops/internal/integrations/sops"
	_ "github.com/wireops/wireops/internal/integrations/traefik"
	_ "github.com/wireops/wireops/internal/integrations/vault"
	_ "github.com/wireops/wireops/internal/integrations/webhook"
)

// TestAllRegisteredDescriptorsAreValid runs ValidateDescriptor against every
// entry in the real global registry (populated by the blank imports above),
// plus re-checks slug uniqueness defensively (Register already panics on a
// duplicate slug at init time, so reaching this point without a panic
// already proves it — this is belt-and-suspenders).
func TestAllRegisteredDescriptorsAreValid(t *testing.T) {
	all := integrations.All()
	if len(all) != 13 {
		t.Fatalf("expected 13 registered integrations, got %d", len(all))
	}

	seen := make(map[string]bool, len(all))
	for _, entry := range all {
		d := entry.Descriptor
		if seen[d.Slug] {
			t.Errorf("duplicate slug %q in registry", d.Slug)
		}
		seen[d.Slug] = true

		if err := integrations.ValidateDescriptor(d); err != nil {
			t.Errorf("descriptor %q failed validation: %v", d.Slug, err)
		}
	}
}
