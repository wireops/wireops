package s3

import (
	"github.com/wireops/wireops/internal/integrations"
)

// S3Integration exposes S3-compatible object storage as a Storage Backend
// integration. Its connection config (bucket/region/endpoint/access
// key/secret, plus prefix and optional KMS envelope-encryption settings) is
// stored in the integrations collection and consumed by
// internal/backup.buildRemoteStorage to mirror backups off-host — it has no
// container actions of its own, same as the Vault/Infisical secret-backend
// integrations.
type S3Integration struct{}

func init() {
	integrations.Register(&S3Integration{})
}

// Slug returns the unique identifier for this integration.
func (s *S3Integration) Slug() string {
	return "s3"
}

// Name returns the human-readable name of the integration.
func (s *S3Integration) Name() string {
	return "S3 Storage"
}

// Category returns the category of the integration.
func (s *S3Integration) Category() string {
	return "Storage Backend"
}

// ResolveContainerActions returns no container actions — this is a storage
// backend, not a container-action integration.
func (s *S3Integration) ResolveContainerActions(config map[string]interface{}, ctx integrations.ContainerContext) []integrations.ContainerAction {
	return nil
}
