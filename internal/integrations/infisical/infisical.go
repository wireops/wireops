package infisical

import (
	"github.com/wireops/wireops/internal/integrations"
)

// descriptor describes Infisical as a Secret Backend integration. Its
// connection config (site_url/client_id/client_secret, plus the
// allowed_project_id scoping field) is stored in the integrations
// collection and consumed by internal/secrets.InfisicalSecretProvider — it
// has no container actions and no registered capability implementation
// (Impl is nil).
var descriptor = integrations.Descriptor{
	Slug:     "infisical",
	Name:     "Infisical",
	Category: integrations.CategorySecretBackend,
	Fields: []integrations.ConfigField{
		{Key: "site_url", Kind: integrations.FieldURL},
		{Key: "client_id", Kind: integrations.FieldText, Required: true},
		{Key: "client_secret", Kind: integrations.FieldPassword, Required: true, Sensitive: true, Encrypted: true},
		{Key: "allowed_project_id", Kind: integrations.FieldText},
	},
	Capabilities: []integrations.CapabilityID{integrations.CapSecretResolver, integrations.CapBrowsable, integrations.CapTestable},
}

func init() {
	integrations.Register(descriptor, nil)
}
