package prompts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	mcpauth "github.com/wireops/wireops/mcp/auth"
	"github.com/wireops/wireops/mcp/client"
)

func ctxWithKey() context.Context {
	return mcpauth.WithAPIKey(context.Background(), "wireops_sk_test")
}

func TestDiagnoseStackFailureRequiresStackID(t *testing.T) {
	handler := diagnoseStackFailure(client.New("http://unused"))
	_, err := handler(ctxWithKey(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Name: "diagnose_stack_failure", Arguments: map[string]string{}},
	})
	if err == nil {
		t.Fatal("expected error when stack_id argument is missing")
	}
}

func TestDiagnoseStackFailureMissingAPIKey(t *testing.T) {
	handler := diagnoseStackFailure(client.New("http://unused"))
	_, err := handler(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Name: "diagnose_stack_failure", Arguments: map[string]string{"stack_id": "stack1"}},
	})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
	}
}

func TestDiagnoseStackFailureEscapesStackIDInFilter(t *testing.T) {
	var syncLogsQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/sync_logs/") {
			syncLogsQuery = r.URL.RawQuery
			w.Write([]byte(`{"items":[]}`))
			return
		}
		w.Write([]byte(`{"id":"stack1"}`))
	}))
	defer srv.Close()

	handler := diagnoseStackFailure(client.New(srv.URL))
	// A stack_id argument containing a bare quote must not be able to close
	// the filter's string literal early and splice in extra clauses.
	maliciousID := `stack1' || status='success`
	_, err := handler(ctxWithKey(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Name: "diagnose_stack_failure", Arguments: map[string]string{"stack_id": maliciousID}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	unescaped, err := url.QueryUnescape(syncLogsQuery)
	if err != nil {
		t.Fatalf("unescape query: %v", err)
	}
	if strings.Contains(unescaped, "filter=stack='stack1' ||") {
		t.Fatalf("stack id was not escaped, filter injection possible: %s", unescaped)
	}
	if !strings.Contains(unescaped, `stack1\' || status=\'success`) {
		t.Fatalf("expected escaped quotes in filter, got: %s", unescaped)
	}
}

func TestDiagnoseStackFailureFetchesStatusAndLogs(t *testing.T) {
	var paths []string
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		queries = append(queries, r.URL.RawQuery)
		switch {
		case strings.Contains(r.URL.Path, "/stacks/records/"):
			w.Write([]byte(`{"id":"stack1","status":"error"}`))
		default:
			w.Write([]byte(`{"items":[{"status":"error","output":"image not found"}]}`))
		}
	}))
	defer srv.Close()

	handler := diagnoseStackFailure(client.New(srv.URL))
	result, err := handler(ctxWithKey(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{Name: "diagnose_stack_failure", Arguments: map[string]string{"stack_id": "stack1"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("expected 2 REST calls (status + logs), got %d: %v", len(paths), paths)
	}
	if paths[0] != "/api/collections/stacks/records/stack1" {
		t.Fatalf("unexpected first call path: %s", paths[0])
	}
	if paths[1] != "/api/collections/sync_logs/records" {
		t.Fatalf("unexpected second call path: %s", paths[1])
	}
	if !strings.Contains(queries[1], "stack1") || !strings.Contains(queries[1], "perPage=5") {
		t.Fatalf("unexpected sync_logs query: %s", queries[1])
	}

	if len(result.Messages) != 1 {
		t.Fatalf("expected exactly 1 message, got %d", len(result.Messages))
	}
	text, ok := result.Messages[0].Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Messages[0].Content)
	}
	if !strings.Contains(text.Text, "stack1") || !strings.Contains(text.Text, "image not found") {
		t.Fatalf("expected prompt text to embed fetched data, got: %s", text.Text)
	}
}
