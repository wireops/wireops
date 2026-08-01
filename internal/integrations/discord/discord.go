package discord

import (
	"github.com/wireops/wireops/internal/integrations"
)

// descriptor describes Discord as a Notification integration. Its config is
// consumed by internal/notify (BuildConfig/NewProvider) — it has no
// container actions and no registered capability implementation (Impl is
// nil), same as the other notification stubs.
var descriptor = integrations.Descriptor{
	Slug:     "discord",
	Name:     "Discord",
	Category: integrations.CategoryNotification,
	Fields: []integrations.ConfigField{
		{Key: "url", Kind: integrations.FieldURL, Sensitive: true},
		{Key: "username", Kind: integrations.FieldText},
		{Key: "avatar_url", Kind: integrations.FieldURL},
		{Key: "mention_on_error", Kind: integrations.FieldBool},
		{Key: "role_id", Kind: integrations.FieldText},
		{Key: "events", Kind: integrations.FieldList},
	},
	Capabilities: []integrations.CapabilityID{integrations.CapNotifier, integrations.CapTestable},
}

func init() {
	integrations.Register(descriptor, nil)
}
