package slack

import (
	"github.com/wireops/wireops/internal/integrations"
)

// descriptor describes Slack as a Notification integration. Its config is
// consumed by internal/notify (BuildConfig/NewProvider) — it has no
// container actions and no registered capability implementation (Impl is
// nil), same as the other notification stubs.
var descriptor = integrations.Descriptor{
	Slug:     "slack",
	Name:     "Slack",
	Category: integrations.CategoryNotification,
	Fields: []integrations.ConfigField{
		{Key: keyURL, Kind: integrations.FieldURL, Required: true, Sensitive: true, Encrypted: true},
		{Key: keyMentionOnError, Kind: integrations.FieldBool},
		{Key: keyMentionText, Kind: integrations.FieldText},
		{Key: keyEvents, Kind: integrations.FieldList},
	},
	Capabilities: []integrations.CapabilityID{integrations.CapNotifier, integrations.CapTestable},
}

func init() {
	integrations.Register(descriptor, nil)
}
