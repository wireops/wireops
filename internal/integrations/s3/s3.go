package s3

import (
	"github.com/wireops/wireops/internal/integrations"
)

// descriptor describes S3-compatible object storage as a Storage Backend
// integration. Its connection config (bucket/region/endpoint/access
// key/secret, plus prefix and optional KMS envelope-encryption settings) is
// stored in the integrations collection and consumed by
// internal/backup.buildRemoteStorage to mirror backups off-host — it has no
// container actions and no registered capability implementation (Impl is
// nil), same as the Vault/Infisical secret-backend integrations.
var descriptor = integrations.Descriptor{
	Slug:     "s3",
	Name:     "S3 Storage",
	Category: integrations.CategoryStorageBackend,
	Fields: []integrations.ConfigField{
		{Key: keyBucket, Kind: integrations.FieldText, Required: true},
		{Key: keyRegion, Kind: integrations.FieldText, Required: true},
		{Key: keyEndpoint, Kind: integrations.FieldURL},
		{Key: keyPrefix, Kind: integrations.FieldText},
		{Key: keyForcePathStyle, Kind: integrations.FieldBool},
		{Key: keyAccessKey, Kind: integrations.FieldText, Required: true},
		{Key: keySecret, Kind: integrations.FieldPassword, Required: true, Sensitive: true, Encrypted: true},
		{Key: keyKMSEnabled, Kind: integrations.FieldBool},
		{Key: keyKMSKeyID, Kind: integrations.FieldText},
		{Key: keyKMSRegion, Kind: integrations.FieldText},
		{Key: keyEncryptContent, Kind: integrations.FieldBool},
	},
	Capabilities: []integrations.CapabilityID{integrations.CapStorageBackend, integrations.CapTestable},
}

func init() {
	integrations.Register(descriptor, nil)
}
