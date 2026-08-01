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
		{Key: "url", Kind: integrations.FieldURL, Sensitive: true},
		{Key: "mention_on_error", Kind: integrations.FieldBool},
		{Key: "mention_text", Kind: integrations.FieldText},
		{Key: "events", Kind: integrations.FieldList},
	},
	Capabilities: []integrations.CapabilityID{integrations.CapNotifier, integrations.CapTestable},
}

func init() {
	integrations.Register(descriptor, nil)
}
