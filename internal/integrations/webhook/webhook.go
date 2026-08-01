package webhook

import (
	"github.com/wireops/wireops/internal/integrations"
)

// descriptor describes Webhook as a Notification integration. Its config is
// consumed by internal/notify (BuildConfig/NewProvider) — it has no
// container actions and no registered capability implementation (Impl is
// nil), same as the other notification stubs.
var descriptor = integrations.Descriptor{
	Slug:     "webhook",
	Name:     "Webhook",
	Category: integrations.CategoryNotification,
	Fields: []integrations.ConfigField{
		{Key: keyURL, Kind: integrations.FieldURL},
		{Key: keySecret, Kind: integrations.FieldPassword, Sensitive: true, Encrypted: true},
		{Key: keyHeaders, Kind: integrations.FieldKV, Sensitive: true, Encrypted: true},
		{Key: keyEvents, Kind: integrations.FieldList},
	},
	Capabilities: []integrations.CapabilityID{integrations.CapNotifier, integrations.CapTestable},
}

func init() {
	integrations.Register(descriptor, nil)
}
