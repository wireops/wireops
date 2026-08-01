package ntfy

import (
	"github.com/wireops/wireops/internal/integrations"
)

// descriptor describes Ntfy as a Notification integration. Its config is
// consumed by internal/notify (BuildConfig/NewProvider) — it has no
// container actions and no registered capability implementation (Impl is
// nil), same as the other notification stubs.
var descriptor = integrations.Descriptor{
	Slug:     "ntfy",
	Name:     "Ntfy",
	Category: integrations.CategoryNotification,
	Fields: []integrations.ConfigField{
		{Key: "url", Kind: integrations.FieldURL},
		{Key: "secret", Kind: integrations.FieldPassword, Sensitive: true},
		{Key: "user", Kind: integrations.FieldText},
		{Key: "topic", Kind: integrations.FieldText},
		{Key: "template", Kind: integrations.FieldText},
		{Key: "events", Kind: integrations.FieldList},
	},
	Capabilities: []integrations.CapabilityID{integrations.CapNotifier, integrations.CapTestable},
}

func init() {
	integrations.Register(descriptor, nil)
}
