package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wireops/wireops/mcp/client"
)

// newHintedSession connects a client to the real tool set with the hint
// middleware installed. Unlike the other tool tests, which call a
// ToolHandlerFor closure directly with an already-typed struct, this goes
// through the SDK's own argument validation — which is the thing being tested,
// and which no test reached before.
//
// The client points at a dead address on purpose: a call that passes
// validation must not be able to reach anything.
func newHintedSession(t *testing.T) *mcp.ClientSession {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{Name: "wireops-mcp-test", Version: "test"}, nil)
	Register(server, client.New("http://127.0.0.1:1"), AllTools())
	server.AddReceivingMiddleware(ValidationHintMiddleware())

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx := t.Context()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connecting server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connecting client: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	return clientSession
}

func callToolText(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	if !result.IsError {
		t.Fatalf("CallTool(%s) succeeded, want a validation error", name)
	}
	var b strings.Builder
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}

// TestValidationErrorNamesTheCanonicalParameter is issue #178 item 3: the
// rejection used to list only the names it refused, with no pointer at the one
// the tool does take.
func TestValidationErrorNamesTheCanonicalParameter(t *testing.T) {
	session := newHintedSession(t)

	text := callToolText(t, session, "get_inventory", map[string]any{
		"worker": "worker-1",
		"lines":  "50",
	})

	// The original message is still there — a client may be matching on it.
	if !strings.Contains(text, "unexpected additional properties") {
		t.Fatalf("original validation message was replaced: %q", text)
	}
	for _, want := range []string{"hint:", `"lines"`, `"tail"`, "get_inventory", "tools/list"} {
		if !strings.Contains(text, want) {
			t.Fatalf("validation error does not mention %s: %q", want, text)
		}
	}
	if strings.Contains(text, "those names") {
		t.Fatalf("single rejected name should read as %q: %q", "that name", text)
	}
}

// TestValidationErrorNamesEveryKnownAlias covers the reported example, where
// an agent sent a whole get_container_logs call shape to a tool that takes
// none of it.
func TestValidationErrorNamesEveryKnownAlias(t *testing.T) {
	session := newHintedSession(t)

	text := callToolText(t, session, "get_inventory", map[string]any{
		"lines":          "50",
		"container_name": "web",
	})

	for _, want := range []string{`"lines" stands for "tail"`, `"container_name" stands for "container_id"`, "those names"} {
		if !strings.Contains(text, want) {
			t.Fatalf("validation error does not mention %s: %q", want, text)
		}
	}
	// Sorted, because the upstream list ranges over a map: container_name
	// before lines, every time.
	if strings.Index(text, `"container_name" stands for`) > strings.Index(text, `"lines" stands for`) {
		t.Fatalf("hint is not in sorted order: %q", text)
	}
}

// TestValidationErrorWithoutAKnownAliasIsLeftAlone keeps the hint from
// becoming noise on a plain typo, which it has nothing useful to say about.
func TestValidationErrorWithoutAKnownAliasIsLeftAlone(t *testing.T) {
	session := newHintedSession(t)

	text := callToolText(t, session, "get_inventory", map[string]any{"nonsense": "x"})

	if !strings.Contains(text, "unexpected additional properties") {
		t.Fatalf("want the original validation message, got %q", text)
	}
	if strings.Contains(text, "hint:") {
		t.Fatalf("hint added for a name wireops knows nothing about: %q", text)
	}
}

// TestAliasOnItsOwnToolStillValidates guards the other direction: the aliases
// are real fields on the tools that document them, so they must not start
// failing validation.
func TestAliasOnItsOwnToolStillValidates(t *testing.T) {
	session := newHintedSession(t)

	// Reaches the handler and fails on the dead address rather than on
	// validation, which is the point — and that failure must carry no hint.
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "get_container_logs",
		Arguments: map[string]any{"stack": "app", "container_name": "web", "lines": "50", "search": "oom"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var b strings.Builder
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	text := b.String()
	if strings.Contains(text, "unexpected additional properties") {
		t.Fatalf("get_container_logs rejected its own documented aliases: %q", text)
	}
	if strings.Contains(text, "hint:") {
		t.Fatalf("hint added to an error that is not an unexpected-properties failure: %q", text)
	}
}

// TestAliasHintFor covers the message builder directly, including the shapes
// the end-to-end tests above cannot easily produce.
func TestAliasHintFor(t *testing.T) {
	const prefix = `validating "arguments": validating root: `

	cases := []struct {
		name    string
		message string
		tool    string
		want    string
	}{
		{
			name:    "NotAValidationFailure",
			message: "connection refused",
			tool:    "get_inventory",
			want:    "",
		},
		{
			name:    "EmptyToolNameFallsBackToThisTool",
			message: prefix + `unexpected additional properties ["lines"]`,
			want:    "this tool does not accept that name",
		},
		{
			name:    "MixesKnownAndUnknownNamesAndOnlyExplainsTheKnownOnes",
			message: prefix + `unexpected additional properties ["nonsense" "search"]`,
			tool:    "get_inventory",
			want:    `"search" stands for "pattern"`,
		},
		{
			name:    "AllThreeAliasesReadAsAList",
			message: prefix + `unexpected additional properties ["stack" "lines" "search"]`,
			tool:    "get_inventory",
			want:    `"lines" stands for "tail", "search" stands for "pattern" and "stack" stands for "stack_id"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := aliasHintFor(tc.message, tc.tool)
			if tc.want == "" {
				if got != "" {
					t.Fatalf("aliasHintFor = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("aliasHintFor = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

// TestAliasTableIsAdvertisedOnTheToolsThatDocumentIt is the drift guard: every
// entry in canonicalByAlias has to be a real pair of schema properties on
// get_container_logs, which is the one tool that carries all four. A rename in
// mcp/models that the table stops matching fails here rather than silently
// producing a hint naming a parameter that no longer exists.
func TestAliasTableIsAdvertisedOnTheToolsThatDocumentIt(t *testing.T) {
	var logs *mcp.Tool
	for _, tool := range listRegisteredTools(t) {
		if tool.Name == "get_container_logs" {
			logs = tool
		}
	}
	if logs == nil {
		t.Fatal("get_container_logs is not registered")
	}
	properties := schemaProperties(t, logs)

	if len(canonicalByAlias) == 0 {
		t.Fatal("canonicalByAlias is empty")
	}
	for alias, canonical := range canonicalByAlias {
		if _, ok := properties[alias]; !ok {
			t.Errorf("get_container_logs does not advertise the alias %q", alias)
		}
		if _, ok := properties[canonical]; !ok {
			t.Errorf("get_container_logs does not advertise the canonical %q that %q stands for", canonical, alias)
		}
	}
}
