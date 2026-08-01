package integrations

// CapabilityID identifies one behavior an integration can implement. Only
// CapActionProvider has a real backing Go interface in this phase (the
// container-action integrations: caddy, dozzle, nginx-proxy-manager,
// traefik) — the rest are declared here so descriptors can express intent
// (e.g. "this is a notifier") ahead of phase 3 wiring real
// Notifier/SecretResolver/StorageBackend interfaces to them.
type CapabilityID string

const (
	CapActionProvider CapabilityID = "action_provider"
	CapNotifier       CapabilityID = "notifier"
	CapSecretResolver CapabilityID = "secret_resolver"
	CapStorageBackend CapabilityID = "storage_backend"
	CapBrowsable      CapabilityID = "browsable"
	CapTestable       CapabilityID = "testable"
	CapDynamicEnabled CapabilityID = "dynamic_enabled"
)

// ScopeKind identifies what kind of entity an ActionScope describes.
type ScopeKind string

const (
	ScopeContainer ScopeKind = "container"
	ScopeWorker    ScopeKind = "worker"
	ScopeStack     ScopeKind = "stack"
)

// ActionScope carries the entity an ActionProvider is being asked to
// evaluate. Only ScopeContainer is populated by any current caller
// (GET /api/custom/stacks/{id}/integration-actions); ScopeWorker/ScopeStack
// exist so the shape doesn't need to change again when a caller for those
// arrives.
type ActionScope struct {
	Kind   ScopeKind
	ID     string
	Name   string
	Labels map[string]string
}

// Action is a UI action to be displayed for a given scope (e.g. a container).
// Field names/JSON tags are identical to the old ContainerAction — see
// integration.go — since internal/routes/integrations_golden_test.go pins
// this exact wire shape.
type Action struct {
	IntegrationSlug string     `json:"integration_slug"`
	Kind            ActionKind `json:"kind"`
	Label           string     `json:"label"`
	URL             string     `json:"url"`
	Icon            string     `json:"icon,omitempty"`
}

// Config is the untyped integration config map shared by every capability
// interface — an alias, not a new type, so every existing provider
// signature (map[string]interface{}) keeps compiling unchanged.
type Config = map[string]interface{}

// ActionProvider is implemented by integrations that can resolve UI actions
// for a given scope (today: always a container). The 4 real
// ResolveContainerActions-based integrations satisfy this via
// LegacyActionAdapter rather than implementing it directly.
type ActionProvider interface {
	ResolveActions(cfg Config, scope ActionScope) []Action
}
