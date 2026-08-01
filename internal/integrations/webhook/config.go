package webhook

import "github.com/wireops/wireops/internal/integrations"

// Config field keys, referenced by both descriptor.Fields[i].Key and parse
// below so the two can never drift out of sync.
const (
	keyURL     = "url"
	keySecret  = "secret"
	keyHeaders = "headers"
	keyEvents  = "events"
)

// Header is one custom HTTP header the webhook provider sends alongside its
// payload.
type Header struct {
	Key   string
	Value string
}

// config is the typed shape of webhook's config map.
type config struct {
	URL     string
	Secret  string
	Headers []Header
	Events  []string
}

// parse converts a generic integrations.Config into a typed config.
// Individual missing/malformed optional fields are tolerated (left at their
// zero value) rather than erroring, matching the tolerant parsing
// internal/notify.BuildConfig already does for this same shape.
func parse(c integrations.Config) (config, error) {
	var cfg config
	cfg.URL, _ = c[keyURL].(string)
	cfg.Secret, _ = c[keySecret].(string)

	if headersRaw, ok := c[keyHeaders].([]interface{}); ok {
		for _, h := range headersRaw {
			hMap, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			key, _ := hMap["key"].(string)
			val, _ := hMap["value"].(string)
			if key != "" {
				cfg.Headers = append(cfg.Headers, Header{Key: key, Value: val})
			}
		}
	}

	if eventsRaw, ok := c[keyEvents].([]interface{}); ok {
		for _, e := range eventsRaw {
			if eStr, ok := e.(string); ok {
				cfg.Events = append(cfg.Events, eStr)
			}
		}
	}

	return cfg, nil
}
