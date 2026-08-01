package sops

import (
	"github.com/wireops/wireops/internal/integrations"
)

// descriptor describes SOPS+age as a Secret Backend integration. Unlike
// Vault/Infisical, it has no connection config to configure — each
// repository gets its own auto-generated age keypair
// (internal/hooks/pb_hooks.go), and internal/sync.Reconciler decrypts
// secrets.yaml automatically whenever one is found next to a stack's
// wireops.yaml. Its integrations row is seeded locked+enabled by migration
// 53 and can't be toggled off (see internal/routes
// routeRegistrar.registerIntegrationRoutes). Locked:true here plus zero
// Fields/Capabilities reflects that it has nothing to configure or enable.
var descriptor = integrations.Descriptor{
	Slug:     "sops",
	Name:     "SOPS",
	Category: integrations.CategorySecretBackend,
	Locked:   true,
}

func init() {
	integrations.Register(descriptor, nil)
}
