package resources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wireops/wireops/internal/auth"
	mcpauth "github.com/wireops/wireops/mcp/auth"
	"github.com/wireops/wireops/mcp/client"
)

func ctxWithKey() context.Context {
	return mcpauth.WithAPIKey(context.Background(), "wireops_sk_test")
}

func TestExtractID(t *testing.T) {
	prefix, suffix, _ := strings.Cut(stackComposeURITemplate, "{id}")

	id, ok := extractID("wireops://stacks/abc123/compose", prefix, suffix)
	if !ok || id != "abc123" {
		t.Fatalf("expected id=abc123 ok=true, got id=%q ok=%v", id, ok)
	}

	if _, ok := extractID("wireops://stacks//compose", prefix, suffix); ok {
		t.Fatal("expected empty id segment to be rejected")
	}
	if _, ok := extractID("wireops://stacks/a/b/compose", prefix, suffix); ok {
		t.Fatal("expected id segment containing '/' to be rejected")
	}
	if _, ok := extractID("wireops://jobs/abc123/raw", prefix, suffix); ok {
		t.Fatal("expected mismatched template to be rejected")
	}
}

func TestReadStackComposeReturnsContent(t *testing.T) {
	var gotPath, gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get(auth.APIKeyHeader)
		w.Write([]byte(`{"content":"services:\n  web: {}","filename":"docker-compose.yml"}`))
	}))
	defer srv.Close()

	handler := readStackCompose(client.New(srv.URL))
	result, err := handler(ctxWithKey(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "wireops://stacks/stack0000000001/compose"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/stacks/stack0000000001/compose" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotHeader != "wireops_sk_test" {
		t.Fatalf("expected API key forwarded, got %q", gotHeader)
	}
	if len(result.Contents) != 1 || result.Contents[0].Text != "services:\n  web: {}" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Contents[0].MIMEType != "text/plain" {
		t.Fatalf("unexpected mime type: %s", result.Contents[0].MIMEType)
	}
}

func TestReadStackComposeMissingAPIKey(t *testing.T) {
	handler := readStackCompose(client.New("http://unused"))
	_, err := handler(context.Background(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "wireops://stacks/stack0000000001/compose"},
	})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
	}
}

func TestReadStackComposeMalformedURI(t *testing.T) {
	handler := readStackCompose(client.New("http://unused"))
	_, err := handler(ctxWithKey(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "wireops://jobs/stack0000000001/raw"},
	})
	if err == nil {
		t.Fatal("expected resource-not-found error for mismatched URI")
	}
}

func TestReadJobRawReturnsContent(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"content":"name: nightly-backup","filename":"job.yaml"}`))
	}))
	defer srv.Close()

	handler := readJobRaw(client.New(srv.URL))
	result, err := handler(ctxWithKey(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "wireops://jobs/job000000000001/raw"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/jobs/job000000000001/raw" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if len(result.Contents) != 1 || result.Contents[0].Text != "name: nightly-backup" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestReadStackLiveLogsReturnsLatestOutput(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"items":[{"output":"deploy ok","status":"success"}]}`))
	}))
	defer srv.Close()

	handler := readStackLiveLogs(client.New(srv.URL))
	result, err := handler(ctxWithKey(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "wireops://stacks/stack0000000001/logs/live"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/collections/sync_logs/records" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if !strings.Contains(gotQuery, "stack0000000001") || !strings.Contains(gotQuery, "perPage=1") {
		t.Fatalf("unexpected query: %s", gotQuery)
	}
	if len(result.Contents) != 1 || result.Contents[0].Text != "deploy ok" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestReadStackLiveLogsEscapesStackIDInFilter(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	handler := readStackLiveLogs(client.New(srv.URL))
	// A stack id containing a bare quote must not be able to close the
	// filter's string literal early and splice in extra clauses.
	_, err := handler(ctxWithKey(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "wireops://stacks/stack0000000001' || status='success/logs/live"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	unescaped, err := url.QueryUnescape(gotQuery)
	if err != nil {
		t.Fatalf("unescape query: %v", err)
	}
	if strings.Contains(unescaped, "filter=stack='stack0000000001' ||") {
		t.Fatalf("stack id was not escaped, filter injection possible: %s", unescaped)
	}
	if !strings.Contains(unescaped, `stack0000000001\' || status=\'success`) {
		t.Fatalf("expected escaped quotes in filter, got: %s", unescaped)
	}
}

func TestReadStackLiveLogsNoRecordsYieldsEmptyText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	handler := readStackLiveLogs(client.New(srv.URL))
	result, err := handler(ctxWithKey(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "wireops://stacks/stack0000000001/logs/live"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Contents[0].Text != "" {
		t.Fatalf("expected empty text when no sync_logs exist, got %q", result.Contents[0].Text)
	}
}

func TestComposeConventionsResourceCoversTheRenderRules(t *testing.T) {
	handler := readComposeConventions()
	result, err := handler(context.Background(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: composeConventionsURI},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Contents) != 1 {
		t.Fatalf("expected one content part, got %d", len(result.Contents))
	}
	text := result.Contents[0].Text

	// These rules are enforced at render time, so a stack that violates one
	// fails to sync — the document is only useful if it states all of them.
	for _, want := range []string{
		"customization.image.slug", "selfh.st/icons",
		"dev.wireops.config.<name>", "annotations:", "recursively", "symlink",
		"1MB", "5MB", `"../"`,
		"customization.init", "exits non-zero",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("conventions resource should explain %q", want)
		}
	}
}

func TestComposeConventionsResourceRejectsAnotherURI(t *testing.T) {
	handler := readComposeConventions()
	if _, err := handler(context.Background(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "wireops://conventions/other"},
	}); err == nil {
		t.Fatal("expected an error for an unknown URI")
	}
}
