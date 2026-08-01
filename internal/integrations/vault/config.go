package vault

import "github.com/wireops/wireops/internal/integrations"

// Config field keys, referenced by both descriptor.Fields[i].Key and parse
// below so the two can never drift out of sync.
const (
	keyAddress      = "address"
	keyToken        = "token"
	keyAllowedMount = "allowed_mount"
)

// config is the typed shape of vault's connection config map.
type config struct {
	Address      string
	Token        string
	AllowedMount string
}

// parse converts a generic integrations.Config into a typed config.
func parse(c integrations.Config) (config, error) {
	var cfg config
	cfg.Address, _ = c[keyAddress].(string)
	cfg.Token, _ = c[keyToken].(string)
	cfg.AllowedMount, _ = c[keyAllowedMount].(string)
	return cfg, nil
}
