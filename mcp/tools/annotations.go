package tools

import "github.com/modelcontextprotocol/go-sdk/mcp"

// The MCP spec defaults an unannotated tool to destructiveHint=true and
// openWorldHint=true, i.e. "assume the worst". Every tool here is annotated
// explicitly instead, so a client can tell at tools/list time which calls are
// safe to make unattended and which deserve a human confirmation prompt.
//
// All three helpers set OpenWorldHint=true: even the pure generators describe
// real-world docker images, and everything else talks to a live wireops
// instance whose state this process does not control.

func boolPtr(v bool) *bool { return &v }

// readOnly annotates a tool that never changes wireops state.
func readOnly() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    true,
		DestructiveHint: boolPtr(false),
		IdempotentHint:  true,
		OpenWorldHint:   boolPtr(true),
	}
}

// additive annotates a write tool that only adds to or advances state —
// nothing a caller would need to undo. idempotent reports whether calling it
// twice with the same arguments is indistinguishable from calling it once.
func additive(idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		DestructiveHint: boolPtr(false),
		IdempotentHint:  idempotent,
		OpenWorldHint:   boolPtr(true),
	}
}

// destructive annotates a write tool that can remove or overwrite something a
// caller cares about — stopping a container, replacing env vars, deleting a
// stack. Clients are expected to confirm these with a human; the tools that
// can't be undone at all additionally enforce requireConfirm server-side.
func destructive(idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		DestructiveHint: boolPtr(true),
		IdempotentHint:  idempotent,
		OpenWorldHint:   boolPtr(true),
	}
}
