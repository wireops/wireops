package vault

import (
	"github.com/wireops/wireops/internal/integrations"
)

// descriptor describes HashiCorp Vault as a Secret Backend integration. Its
// connection config (address/token, plus the allowed_mount scoping field)
// is stored in the integrations collection and consumed by
// internal/secrets.VaultSecretProvider — it has no container actions and no
// registered capability implementation (Impl is nil).
var descriptor = integrations.Descriptor{
	Slug:     "vault",
	Name:     "HashiCorp Vault",
	Category: integrations.CategorySecretBackend,
	Fields: []integrations.ConfigField{
		{Key: keyAddress, Kind: integrations.FieldURL, Required: true},
		{Key: keyToken, Kind: integrations.FieldPassword, Required: true, Sensitive: true, Encrypted: true},
		{Key: keyAllowedMount, Kind: integrations.FieldText},
	},
	Capabilities: []integrations.CapabilityID{integrations.CapSecretResolver, integrations.CapBrowsable, integrations.CapTestable},
}

func init() {
	integrations.Register(descriptor, nil)
}
