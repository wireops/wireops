package integrations

// legacyActionAdapter wraps an old-style Integration (ResolveContainerActions)
// so it satisfies the new ActionProvider interface (ResolveActions), without
// touching any of the 4 real providers' logic. Non-container scopes are
// ignored (return nil) since ResolveContainerActions has no concept of them.
type legacyActionAdapter struct {
	impl Integration
}

// LegacyActionAdapter wraps i so it satisfies ActionProvider.
func LegacyActionAdapter(i Integration) ActionProvider {
	return legacyActionAdapter{impl: i}
}

func (a legacyActionAdapter) ResolveActions(cfg Config, scope ActionScope) []Action {
	if scope.Kind != ScopeContainer {
		return nil
	}
	ctx := ContainerContext{
		ContainerID:   scope.ID,
		ContainerName: scope.Name,
		Labels:        scope.Labels,
	}
	legacyActions := a.impl.ResolveContainerActions(cfg, ctx)
	if len(legacyActions) == 0 {
		return nil
	}
	actions := make([]Action, 0, len(legacyActions))
	for _, la := range legacyActions {
		actions = append(actions, Action(la))
	}
	return actions
}
