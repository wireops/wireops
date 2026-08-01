package infisical

import "github.com/wireops/wireops/internal/integrations"

// Config field keys, referenced by both descriptor.Fields[i].Key and parse
// below so the two can never drift out of sync.
const (
	keySiteURL          = "site_url"
	keyClientID         = "client_id"
	keyClientSecret     = "client_secret"
	keyAllowedProjectID = "allowed_project_id"
)

// config is the typed shape of infisical's connection config map.
type config struct {
	SiteURL          string
	ClientID         string
	ClientSecret     string
	AllowedProjectID string
}

// parse converts a generic integrations.Config into a typed config.
func parse(c integrations.Config) (config, error) {
	var cfg config
	cfg.SiteURL, _ = c[keySiteURL].(string)
	cfg.ClientID, _ = c[keyClientID].(string)
	cfg.ClientSecret, _ = c[keyClientSecret].(string)
	cfg.AllowedProjectID, _ = c[keyAllowedProjectID].(string)
	return cfg, nil
}
