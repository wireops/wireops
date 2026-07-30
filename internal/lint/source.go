package lint

import (
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ResolveLines annotates findings with the line in the *source* compose file
// their Path points at, so a UI can mark the offending line instead of only
// naming it.
//
// The findings themselves are produced from the resolved config that
// `docker compose config` emits, which carries no position information and is
// not line-for-line the same document as the file on disk (defaults get
// injected, ports and volumes get normalized to their long form). So the
// source is parsed separately into a yaml.Node tree — which does carry
// Line/Column — and each Path is walked against it.
//
// A Path that does not fully exist in the source resolves to the deepest
// prefix that does. That is not a fallback so much as the point: a finding
// like "service web sets no memory limit" has a Path of
// services.web.deploy.resources.limits precisely because that key is absent,
// and the useful thing to highlight is `web:`.
//
// Findings whose path resolves to nothing keep Line 0, meaning "no line".
func ResolveLines(source []byte, findings []Finding) []Finding {
	if len(findings) == 0 {
		return findings
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(source, &doc); err != nil {
		// An unparseable source is not an error here: the caller already has
		// the findings, they just do not get line numbers.
		return findings
	}

	root := &doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}

	out := make([]Finding, len(findings))
	copy(out, findings)
	for i := range out {
		if out[i].Path == "" {
			continue
		}
		out[i].Line = deepestLine(root, out[i].Path)
	}
	return out
}

// deepestLine walks path through node and returns the line of the deepest
// element it could reach, or 0 if it could not match even the first segment.
func deepestLine(node *yaml.Node, path string) int {
	line := 0
	current := node
	remaining := path

	for remaining != "" && current != nil {
		next, rest, matchedLine, ok := descend(current, remaining)
		if !ok {
			break
		}
		line = matchedLine
		current = next
		remaining = rest
	}
	return line
}

// descend consumes one path segment against node, returning the node to
// continue from, the unconsumed remainder, and the line the segment matched.
func descend(node *yaml.Node, path string) (next *yaml.Node, rest string, line int, ok bool) {
	if node == nil {
		return nil, "", 0, false
	}
	if node.Kind == yaml.AliasNode && node.Alias != nil {
		node = node.Alias
	}
	if node.Kind != yaml.MappingNode {
		return nil, "", 0, false
	}

	// Compose keys may themselves contain dots — a service can legitimately be
	// called "my.api" — so the segment boundary cannot be found by splitting
	// on ".". Match against the keys that actually exist, longest first, so
	// "my.api" wins over a hypothetical "my".
	type candidate struct {
		key   *yaml.Node
		value *yaml.Node
	}
	var candidates []candidate
	for i := 0; i+1 < len(node.Content); i += 2 {
		candidates = append(candidates, candidate{key: node.Content[i], value: node.Content[i+1]})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return len(candidates[i].key.Value) > len(candidates[j].key.Value)
	})

	for _, c := range candidates {
		key := c.key.Value
		if key == "" || !strings.HasPrefix(path, key) {
			continue
		}
		after := path[len(key):]

		switch {
		case after == "":
			// Exact match: report the key's line, not the value's — for a
			// nested block the key is the line a reader recognises ("web:"),
			// while the value node starts at its first child.
			return c.value, "", c.key.Line, true

		case strings.HasPrefix(after, "."):
			return c.value, after[1:], c.key.Line, true

		case strings.HasPrefix(after, "["):
			elem, elemRest, elemLine, indexed := descendIndices(c.value, after)
			if !indexed {
				continue
			}
			if elemLine == 0 {
				elemLine = c.key.Line
			}
			return elem, elemRest, elemLine, true
		}
	}

	return nil, "", 0, false
}

// descendIndices consumes one or more "[N]" suffixes against a sequence node,
// e.g. the "[0]" of "ports[0]".
func descendIndices(node *yaml.Node, path string) (next *yaml.Node, rest string, line int, ok bool) {
	current := node
	remaining := path
	matchedLine := 0

	for strings.HasPrefix(remaining, "[") {
		end := strings.Index(remaining, "]")
		if end < 0 {
			return nil, "", 0, false
		}
		idx, err := strconv.Atoi(remaining[1:end])
		if err != nil || idx < 0 {
			return nil, "", 0, false
		}
		if current == nil || current.Kind != yaml.SequenceNode || idx >= len(current.Content) {
			// The index is out of range in the source — common when the
			// resolved config normalized or expanded the list. Stop here and
			// let the caller keep the line of the key itself.
			return nil, "", matchedLine, true
		}
		current = current.Content[idx]
		matchedLine = current.Line
		remaining = remaining[end+1:]
	}

	remaining = strings.TrimPrefix(remaining, ".")
	return current, remaining, matchedLine, true
}
