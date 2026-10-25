// Package stackdef holds the small amount of state shared by every package
// that applies a stack's committed wireops definition.
//
// It deliberately imports nothing from the rest of the codebase. internal/hooks
// already imports internal/sync (for Register's *sync.Scheduler parameter), so
// anything both of them need has to live below both of them — without this
// package a reconcile cannot save a definition-managed field at all, which is
// the constraint noted on dispatchTeardownForMigration in internal/routes.
package stackdef

import "sync"

// bypassStacks tracks stack IDs whose wireops-managed fields (compose_path,
// compose_file, deploy_timeout_seconds, ...) are allowed through the stacks
// immutability hook for the duration of a single Save call.
//
// A repository migration, an explicit definition refresh, and a git-driven
// reconcile that re-reads the committed schedule all legitimately change these
// fields. Each is the controlled, server-side equivalent of recreating the
// stack from its git definition, so each takes a narrowly scoped bypass rather
// than weakening the rule for ordinary API callers.
var (
	bypassMu     sync.Mutex
	bypassStacks = map[string]bool{}
)

// WithBypass runs fn with stackID's wireops-managed fields treated as mutable,
// then clears the bypass regardless of fn's outcome.
//
// Callers must already have fetched and parsed a committed definition, and must
// hold the stack lock across the parse and the save — otherwise a definition
// can land halfway through a deploy that already read the previous values.
func WithBypass(stackID string, fn func() error) error {
	bypassMu.Lock()
	bypassStacks[stackID] = true
	bypassMu.Unlock()

	defer func() {
		bypassMu.Lock()
		delete(bypassStacks, stackID)
		bypassMu.Unlock()
	}()

	return fn()
}

// IsBypassed reports whether stackID currently has an active bypass.
func IsBypassed(stackID string) bool {
	bypassMu.Lock()
	defer bypassMu.Unlock()
	return bypassStacks[stackID]
}
