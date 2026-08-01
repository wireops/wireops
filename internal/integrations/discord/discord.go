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
		{Key: keyURL, Kind: integrations.FieldURL, Sensitive: true, Encrypted: true},
		{Key: keyUsername, Kind: integrations.FieldText},
		{Key: keyAvatarURL, Kind: integrations.FieldURL},
		{Key: keyMentionOnError, Kind: integrations.FieldBool},
		{Key: keyRoleID, Kind: integrations.FieldText},
		{Key: keyEvents, Kind: integrations.FieldList},
	},
	Capabilities: []integrations.CapabilityID{integrations.CapNotifier, integrations.CapTestable},
}

func init() {
	integrations.Register(descriptor, nil)
}
