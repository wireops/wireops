package hooks

import "github.com/wireops/wireops/internal/stackdef"

// The bypass registry itself lives in internal/stackdef so that internal/sync
// can take a bypass too: this package already imports internal/sync, so the
// shared state cannot live here. These wrappers keep the existing call sites
// and the hook below reading naturally.

// WithWireopsDefinitionBypass runs fn with stackID's wireops-managed fields
// treated as mutable, then clears the bypass regardless of fn's outcome.
// Callers must already have validated a committed definition and must hold the
// stack lock while saving it.
func WithWireopsDefinitionBypass(stackID string, fn func() error) error {
	return stackdef.WithBypass(stackID, fn)
}

// WithMigrationBypass remains as a compatibility name for the migration
// route. New callers should use WithWireopsDefinitionBypass.
func WithMigrationBypass(stackID string, fn func() error) error {
	return stackdef.WithBypass(stackID, fn)
}

func isWireopsDefinitionBypass(stackID string) bool {
	return stackdef.IsBypassed(stackID)
}
