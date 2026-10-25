package tools

// Parameter names wireops accepts twice over (#176 item 5). Agents reach for
// `stack`, `container_name`, `lines` and `search`, so the tools that take the
// canonical name declare the alias as a second field and resolve the two with
// models.CanonicalOrAlias.
//
// The constants exist so the pairs are written once: canonicalByAlias below
// and every resolution site read from the same names, and a rename in
// mcp/models that these stop matching is caught by
// TestAliasTableIsAdvertisedOnTheToolsThatDocumentIt.
const (
	paramStackID       = "stack_id"
	paramStack         = "stack"
	paramContainerID   = "container_id"
	paramContainerName = "container_name"
	paramTail          = "tail"
	paramLines         = "lines"
	paramPattern       = "pattern"
	paramSearch        = "search"
)

// canonicalByAlias maps each accepted alias to the name it stands for.
//
// It is deliberately global rather than per tool: an alias is only declared on
// the tools that have the canonical parameter, so a tool that has neither
// rejects the alias as an unknown property. That rejection is what
// validation_hint.go reads this table for — to name the canonical parameter
// the caller probably meant, without claiming the tool in hand accepts it.
var canonicalByAlias = map[string]string{
	paramStack:         paramStackID,
	paramContainerName: paramContainerID,
	paramLines:         paramTail,
	paramSearch:        paramPattern,
}
