package hooks

import "sync"

// migrationBypassStacks tracks stack IDs whose wireops-managed fields
// (compose_path, compose_file, wireops_file_path, ...) are allowed through
// validateWireopsFieldsImmutable for the duration of a single Save call.
//
// The stack repo migration endpoint (internal/routes) legitimately changes
// these fields when the destination repository resolves a different
// compose layout from its wireops.yaml — semantically identical to "the
// wireops.yaml changed and the stack was recreated", which is exactly what
// the immutability rule exists to require (see wireopsManagedStackFields
// doc comment). WithMigrationBypass lets that one Save through without
// weakening the rule for any other caller.
var (
	migrationBypassMu     sync.Mutex
	migrationBypassStacks = map[string]bool{}
)

// WithMigrationBypass runs fn with stackID's wireops-managed fields treated
// as mutable, then clears the bypass regardless of fn's outcome.
func WithMigrationBypass(stackID string, fn func() error) error {
	migrationBypassMu.Lock()
	migrationBypassStacks[stackID] = true
	migrationBypassMu.Unlock()

	defer func() {
		migrationBypassMu.Lock()
		delete(migrationBypassStacks, stackID)
		migrationBypassMu.Unlock()
	}()

	return fn()
}

func isMigrationBypass(stackID string) bool {
	migrationBypassMu.Lock()
	defer migrationBypassMu.Unlock()
	return migrationBypassStacks[stackID]
}
