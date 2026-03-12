package integrations

// ActionKind defines what the frontend should do with the action
type ActionKind string

const (
	ActionKindReverseProxy ActionKind = "reverse-proxy"
	ActionKindLog          ActionKind = "log"
	ActionKindSecret       ActionKind = "secret"
)

// ContainerAction represents a UI action to be displayed for a specific container
type ContainerAction struct {
	IntegrationSlug string     `json:"integration_slug"`
	Kind            ActionKind `json:"kind"`
	Label           string     `json:"label"`
	URL             string     `json:"url"`
	Icon            string     `json:"icon,omitempty"` // lucide icon name, e.g., "i-lucide-external-link"
}

// ContainerContext provides the data needed for an integration to evaluate a container
type ContainerContext struct {
	ContainerID   string
	ContainerName string
	Labels        map[string]string
}

// Integration defines the interface that all plugin integrations must implement
type Integration interface {
	// Slug returns the unique identifier for this integration (e.g., "traefik")
	Slug() string

	// Name returns the human-readable name of the integration
	Name() string

	// Category returns the category of the integration (e.g. Reverse Proxy, Logging)
	Category() string

	// ResolveContainerActions inspects a container's context and the integration's global config
	// to return any relevant UI actions.
	ResolveContainerActions(config map[string]interface{}, ctx ContainerContext) []ContainerAction
}
