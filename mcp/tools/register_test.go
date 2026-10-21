package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wireops/wireops/mcp/client"
)

// listRegisteredTools runs the real Register against an in-memory session, so
// the test exercises what a client actually sees — including the JSON schema
// the SDK derives from every input struct, which is generated at AddTool time
// and would otherwise only fail in production.
func listRegisteredTools(t *testing.T) []*mcp.Tool {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{Name: "wireops-mcp-test", Version: "test"}, nil)
	Register(server, client.New("http://127.0.0.1:1"))

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

	var tools []*mcp.Tool
	for tool, err := range clientSession.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("listing tools: %v", err)
		}
		tools = append(tools, tool)
	}
	if len(tools) == 0 {
		t.Fatal("expected Register to add tools")
	}
	return tools
}

func TestRegisterProducesUniqueAnnotatedTools(t *testing.T) {
	tools := listRegisteredTools(t)

	seen := map[string]bool{}
	for _, tool := range tools {
		if seen[tool.Name] {
			t.Errorf("duplicate tool name %q", tool.Name)
		}
		seen[tool.Name] = true

		if tool.Description == "" {
			t.Errorf("tool %q has no description", tool.Name)
		}
		// An unannotated tool reads as destructive + open-world per the spec,
		// which would make a client prompt for confirmation on every read.
		if tool.Annotations == nil {
			t.Errorf("tool %q has no annotations", tool.Name)
			continue
		}
		if tool.Annotations.DestructiveHint == nil {
			t.Errorf("tool %q leaves destructiveHint unset", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %q has no input schema", tool.Name)
		}
	}
}

func TestRegisterCoversEveryOperatorCapability(t *testing.T) {
	tools := listRegisteredTools(t)
	byName := map[string]*mcp.Tool{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}

	// One representative tool per operator capability, so dropping a whole
	// group by accident fails here rather than silently shrinking the surface.
	for _, name := range []string{
		"deploy_stack",      // operate_stacks
		"sync_repository",   // manage_repositories
		"run_job",           // manage_jobs
		"revoke_worker",     // manage_workers
		"exec_in_container", // use_terminal
	} {
		if _, ok := byName[name]; !ok {
			t.Errorf("expected tool %q to be registered", name)
		}
	}
}

func TestRegisterMarksReadToolsReadOnly(t *testing.T) {
	tools := listRegisteredTools(t)
	byName := map[string]*mcp.Tool{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}

	for _, name := range []string{"list_stacks", "get_stack_status", "get_container_logs", "scaffold_stack", "lint_compose"} {
		tool, ok := byName[name]
		if !ok {
			t.Fatalf("expected tool %q to be registered", name)
		}
		if !tool.Annotations.ReadOnlyHint {
			t.Errorf("expected %q to be annotated read-only", name)
		}
	}

	for _, name := range []string{"delete_stack", "stop_container", "revoke_worker", "exec_in_container"} {
		tool, ok := byName[name]
		if !ok {
			t.Fatalf("expected tool %q to be registered", name)
		}
		if tool.Annotations.ReadOnlyHint {
			t.Errorf("expected %q not to be annotated read-only", name)
		}
		if d := tool.Annotations.DestructiveHint; d == nil || !*d {
			t.Errorf("expected %q to be annotated destructive", name)
		}
	}
}

// TestRegisterDocumentsConfirmOnIrreversibleTools keeps the confirm field and
// the description in sync: a caller only learns what to put in confirm from
// the description, so a tool that requires one has to say so.
func schemaProperties(t *testing.T, tool *mcp.Tool) map[string]json.RawMessage {
	t.Helper()
	// InputSchema reaches a client as decoded JSON, not as a typed schema, so
	// read it back the way a client would.
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("tool %q: marshaling input schema: %v", tool.Name, err)
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("tool %q: decoding input schema: %v", tool.Name, err)
	}
	return schema.Properties
}

func TestRegisterDocumentsConfirmOnIrreversibleTools(t *testing.T) {
	tools := listRegisteredTools(t)

	withConfirm := map[string]bool{
		"delete_stack":         true,
		"stop_container":       true,
		"transfer_stack":       true,
		"migrate_stack":        true,
		"delete_repository":    true,
		"delete_job_run":       true,
		"delete_scheduled_job": true,
		"revoke_worker":        true,
		"force_redeploy_stack": true,
		"set_stack_env_vars":   true,
		"set_job_env_vars":     true,
	}

	for _, tool := range tools {
		_, hasConfirm := schemaProperties(t, tool)["confirm"]
		if hasConfirm != withConfirm[tool.Name] {
			t.Errorf("tool %q: confirm field present=%v, expected %v", tool.Name, hasConfirm, withConfirm[tool.Name])
		}
		if hasConfirm && !strings.Contains(tool.Description, "confirm") {
			t.Errorf("tool %q takes a confirm field but never mentions it in its description", tool.Name)
		}
	}
}
