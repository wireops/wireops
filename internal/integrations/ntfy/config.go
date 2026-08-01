package ntfy

import "github.com/wireops/wireops/internal/integrations"

// Config field keys, referenced by both descriptor.Fields[i].Key and parse
// below so the two can never drift out of sync.
const (
	keyURL      = "url"
	keySecret   = "secret"
	keyUser     = "user"
	keyTopic    = "topic"
	keyTemplate = "template"
	keyEvents   = "events"
)

// config is the typed shape of ntfy's config map.
type config struct {
	URL      string
	Secret   string
	User     string
	Topic    string
	Template string
	Events   []string
}

// parse converts a generic integrations.Config into a typed config.
// Individual missing/malformed optional fields are tolerated (left at their
// zero value) rather than erroring, matching the tolerant parsing
// internal/notify.BuildConfig already does for this same shape.
func parse(c integrations.Config) (config, error) {
	var cfg config
	cfg.URL, _ = c[keyURL].(string)
	cfg.Secret, _ = c[keySecret].(string)
	cfg.User, _ = c[keyUser].(string)
	cfg.Topic, _ = c[keyTopic].(string)
	cfg.Template, _ = c[keyTemplate].(string)

	if eventsRaw, ok := c[keyEvents].([]interface{}); ok {
		for _, e := range eventsRaw {
			if eStr, ok := e.(string); ok {
				cfg.Events = append(cfg.Events, eStr)
			}
		}
	}

	return cfg, nil
}
