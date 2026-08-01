package slack

import "github.com/wireops/wireops/internal/integrations"

// Config field keys, referenced by both descriptor.Fields[i].Key and parse
// below so the two can never drift out of sync.
const (
	keyURL            = "url"
	keyMentionOnError = "mention_on_error"
	keyMentionText    = "mention_text"
	keyEvents         = "events"
)

// config is the typed shape of slack's config map.
type config struct {
	URL            string
	MentionOnError bool
	MentionText    string
	Events         []string
}

// parse converts a generic integrations.Config into a typed config.
// Individual missing/malformed optional fields are tolerated (left at their
// zero value) rather than erroring, matching the tolerant parsing
// internal/notify.BuildConfig already does for this same shape.
func parse(c integrations.Config) (config, error) {
	var cfg config
	cfg.URL, _ = c[keyURL].(string)
	cfg.MentionOnError, _ = c[keyMentionOnError].(bool)
	cfg.MentionText, _ = c[keyMentionText].(string)

	if eventsRaw, ok := c[keyEvents].([]interface{}); ok {
		for _, e := range eventsRaw {
			if eStr, ok := e.(string); ok {
				cfg.Events = append(cfg.Events, eStr)
			}
		}
	}

	return cfg, nil
}
