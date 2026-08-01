package discord

import "github.com/wireops/wireops/internal/integrations"

// Config field keys, referenced by both descriptor.Fields[i].Key and parse
// below so the two can never drift out of sync.
const (
	keyURL            = "url"
	keyUsername       = "username"
	keyAvatarURL      = "avatar_url"
	keyMentionOnError = "mention_on_error"
	keyRoleID         = "role_id"
	keyEvents         = "events"
)

// config is the typed shape of discord's config map.
type config struct {
	URL            string
	Username       string
	AvatarURL      string
	MentionOnError bool
	RoleID         string
	Events         []string
}

// parse converts a generic integrations.Config into a typed config.
// Individual missing/malformed optional fields are tolerated (left at their
// zero value) rather than erroring, matching the tolerant parsing
// internal/notify.BuildConfig already does for this same shape.
func parse(c integrations.Config) (config, error) {
	var cfg config
	cfg.URL, _ = c[keyURL].(string)
	cfg.Username, _ = c[keyUsername].(string)
	cfg.AvatarURL, _ = c[keyAvatarURL].(string)
	cfg.MentionOnError, _ = c[keyMentionOnError].(bool)
	cfg.RoleID, _ = c[keyRoleID].(string)

	if eventsRaw, ok := c[keyEvents].([]interface{}); ok {
		for _, e := range eventsRaw {
			if eStr, ok := e.(string); ok {
				cfg.Events = append(cfg.Events, eStr)
			}
		}
	}

	return cfg, nil
}
