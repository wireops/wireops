package s3

import "github.com/wireops/wireops/internal/integrations"

// Config field keys, referenced by both descriptor.Fields[i].Key and parse
// below so the two can never drift out of sync.
const (
	keyBucket         = "bucket"
	keyRegion         = "region"
	keyEndpoint       = "endpoint"
	keyPrefix         = "prefix"
	keyForcePathStyle = "force_path_style"
	keyAccessKey      = "access_key"
	keySecret         = "secret"
	keyKMSEnabled     = "kms_enabled"
	keyEncryptContent = "encrypt_content"
)

// config is the typed shape of s3's connection config map.
type config struct {
	Bucket         string
	Region         string
	Endpoint       string
	Prefix         string
	ForcePathStyle bool
	AccessKey      string
	Secret         string
	KMSEnabled     bool
	EncryptContent bool
}

// parse converts a generic integrations.Config into a typed config.
func parse(c integrations.Config) (config, error) {
	var cfg config
	cfg.Bucket, _ = c[keyBucket].(string)
	cfg.Region, _ = c[keyRegion].(string)
	cfg.Endpoint, _ = c[keyEndpoint].(string)
	cfg.Prefix, _ = c[keyPrefix].(string)
	cfg.ForcePathStyle, _ = c[keyForcePathStyle].(bool)
	cfg.AccessKey, _ = c[keyAccessKey].(string)
	cfg.Secret, _ = c[keySecret].(string)
	cfg.KMSEnabled, _ = c[keyKMSEnabled].(bool)
	cfg.EncryptContent, _ = c[keyEncryptContent].(bool)
	return cfg, nil
}
