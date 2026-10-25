package tools

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// unexpectedPropertiesPattern matches the message a closed-world input schema
// produces for an argument the tool does not declare:
//
//	validating "arguments": validating root: unexpected additional properties ["lines" "container_name"]
//
// It has to be matched as text rather than inspected. google/jsonschema-go
// formats the property list straight into a bare fmt.Errorf
// (jsonschema/validate.go), exposing nothing, and the MCP SDK then re-wraps it
// with %v rather than %w (mcp/server.go), so the error chain is flattened to a
// string before it leaves the SDK. The SDK also returns this *instead of*
// calling the tool handler, which is why the hint is added here, in middleware,
// rather than inside a tool.
var unexpectedPropertiesPattern = regexp.MustCompile(`unexpected additional properties \[([^\]]*)\]`)

// quotedTokenPattern pulls the individual names out of that bracketed list,
// which is Go's %q of a []string.
var quotedTokenPattern = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)

// ValidationHintMiddleware appends the canonical parameter name to a
// validation failure caused by an accepted alias being sent to a tool that
// does not take it (#176 item D, re-reported as #178 item 3).
//
// Before this, a caller that guessed got only the rejected names back:
//
//	validating "arguments": ... unexpected additional properties ["lines" "container_name"]
//
// with nothing pointing at `tail` or `container_id`. The schema itself stays
// closed: the fix is to say what the right name is, not to start accepting the
// wrong one.
func ValidationHintMiddleware() sdkmcp.Middleware {
	return func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
		return func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
			result, err := next(ctx, method, req)
			if err != nil || method != "tools/call" {
				return result, err
			}
			callResult, ok := result.(*sdkmcp.CallToolResult)
			if !ok || !callResult.IsError {
				return result, err
			}
			annotateUnexpectedProperties(callResult, calledToolName(req))
			return callResult, nil
		}
	}
}

// calledToolName reports the tool a tools/call request names, or "" when the
// params are not the shape we expect.
func calledToolName(req sdkmcp.Request) string {
	if params, ok := req.GetParams().(*sdkmcp.CallToolParamsRaw); ok {
		return params.Name
	}
	if params, ok := req.GetParams().(*sdkmcp.CallToolParams); ok {
		return params.Name
	}
	return ""
}

// annotateUnexpectedProperties appends a hint to the first text content that
// carries an unexpected-properties message. The original message is left
// intact ahead of it — a client may well be matching on it.
func annotateUnexpectedProperties(result *sdkmcp.CallToolResult, toolName string) {
	for _, content := range result.Content {
		text, ok := content.(*sdkmcp.TextContent)
		if !ok {
			continue
		}
		hint := aliasHintFor(text.Text, toolName)
		if hint == "" {
			continue
		}
		text.Text += "\n\n" + hint
		return
	}
}

// aliasHintFor builds the hint for message, or returns "" when the message is
// not an unexpected-properties failure or none of the rejected names is an
// alias wireops knows. Saying nothing beats guessing: mcp/tools/refs.go
// already holds the line that an ambiguous reference is an error naming the
// candidates, never a guess.
func aliasHintFor(message, toolName string) string {
	match := unexpectedPropertiesPattern.FindStringSubmatch(message)
	if match == nil {
		return ""
	}

	// Sorted, because the upstream list is built by ranging over a map and so
	// arrives in a different order each time.
	known := make([]string, 0, len(canonicalByAlias))
	for _, token := range quotedTokenPattern.FindAllString(match[1], -1) {
		name, err := strconv.Unquote(token)
		if err != nil {
			continue
		}
		if _, ok := canonicalByAlias[name]; ok {
			known = append(known, name)
		}
	}
	if len(known) == 0 {
		return ""
	}
	sort.Strings(known)

	pairs := make([]string, 0, len(known))
	for _, alias := range known {
		pairs = append(pairs, fmt.Sprintf("%q stands for %q", alias, canonicalByAlias[alias]))
	}

	subject := "this tool"
	if toolName != "" {
		subject = toolName
	}
	names := "that name"
	if len(known) > 1 {
		names = "those names"
	}
	return fmt.Sprintf("hint: elsewhere in wireops %s, but %s does not accept %s — call tools/list for the parameters it does take.",
		joinWithAnd(pairs), subject, names)
}

// joinWithAnd renders a short list as prose: "a", "a and b", "a, b and c".
func joinWithAnd(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
}
