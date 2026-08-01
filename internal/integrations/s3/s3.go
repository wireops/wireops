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
		{Key: "bucket", Kind: integrations.FieldText, Required: true},
		{Key: "region", Kind: integrations.FieldText, Required: true},
		{Key: "endpoint", Kind: integrations.FieldURL},
		{Key: "prefix", Kind: integrations.FieldText},
		{Key: "force_path_style", Kind: integrations.FieldBool},
		{Key: "access_key", Kind: integrations.FieldText, Required: true},
		{Key: "secret", Kind: integrations.FieldPassword, Required: true, Sensitive: true, Encrypted: true},
		{Key: "kms_enabled", Kind: integrations.FieldBool},
		{Key: "encrypt_content", Kind: integrations.FieldBool},
	},
	Capabilities: []integrations.CapabilityID{integrations.CapStorageBackend, integrations.CapTestable},
}

func init() {
	integrations.Register(descriptor, nil)
}
