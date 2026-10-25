package hooks

import "sync"

// definitionBypassStacks tracks stack IDs whose wireops-managed fields
// (compose_path, compose_file, wireops_file_path, ...) are allowed through
// validateWireopsFieldsImmutable for the duration of a single Save call.
//
// A stack repository migration and an explicit refresh from the committed
// wireops definition both legitimately change these fields. They are the
// controlled, server-side equivalent of recreating the stack from its git
// definition, so each gets a narrowly scoped bypass without weakening the
// rule for any ordinary API caller.
var (
	definitionBypassMu     sync.Mutex
	definitionBypassStacks = map[string]bool{}
)

// WithWireopsDefinitionBypass runs fn with stackID's wireops-managed fields
// treated as mutable, then clears the bypass regardless of fn's outcome.
// It is deliberately package-scoped infrastructure: callers must already
// have validated a committed definition and must hold the stack lock while
// saving it.
func WithWireopsDefinitionBypass(stackID string, fn func() error) error {
	definitionBypassMu.Lock()
	definitionBypassStacks[stackID] = true
	definitionBypassMu.Unlock()

	defer func() {
		definitionBypassMu.Lock()
		delete(definitionBypassStacks, stackID)
		definitionBypassMu.Unlock()
	}()

	return fn()
}

// WithMigrationBypass remains as a compatibility name for the migration
// route. New callers should use WithWireopsDefinitionBypass.
func WithMigrationBypass(stackID string, fn func() error) error {
	return WithWireopsDefinitionBypass(stackID, fn)
}

func isWireopsDefinitionBypass(stackID string) bool {
	definitionBypassMu.Lock()
	defer definitionBypassMu.Unlock()
	return definitionBypassStacks[stackID]
}
