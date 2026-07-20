package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wireops/wireops/internal/auth"
	mcpauth "github.com/wireops/wireops/mcp/auth"
	"github.com/wireops/wireops/mcp/client"
	"github.com/wireops/wireops/mcp/models"
)

func ctxWithKey() context.Context {
	return mcpauth.WithAPIKey(context.Background(), "wireops_sk_test")
}

func TestApiKeyFromMissing(t *testing.T) {
	_, err := apiKeyFrom(context.Background())
	if err == nil {
		t.Fatal("expected error when no API key on context")
	}
}

func TestListStacksBuildsRequestAndReturnsOutput(t *testing.T) {
	var gotPath, gotHeader, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get(auth.APIKeyHeader)
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	handler := listStacks(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.ListStacksInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/collections/stacks/records" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotHeader != "wireops_sk_test" {
		t.Fatalf("expected API key forwarded, got %q", gotHeader)
	}
	if !strings.Contains(gotQuery, "perPage=50") {
		t.Fatalf("expected default perPage=50, got query %q", gotQuery)
	}
	if out == nil {
		t.Fatal("expected non-nil output")
	}
}

func TestListStacksAddsNoticeWhenRenderOverridesActive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[{"id":"stack1","render_overrides":{"web":{"image":"nginx:1.28"}}},{"id":"stack2","render_overrides":{}}]}`))
	}))
	defer srv.Close()

	handler := listStacks(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.ListStacksInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map output, got %T", out)
	}
	items, ok := result["items"].([]interface{})
	if !ok || len(items) != 2 {
		t.Fatalf("expected 2 items, got %v", result["items"])
	}
	stack1 := items[0].(map[string]interface{})
	if stack1["_notice"] != renderOverridesNotice {
		t.Fatalf("expected notice on stack1, got %v", stack1["_notice"])
	}
	stack2 := items[1].(map[string]interface{})
	if _, ok := stack2["_notice"]; ok {
		t.Fatalf("expected no notice on stack2 with empty overrides, got %v", stack2["_notice"])
	}
}

func TestListReposBuildsRequestAndReturnsOutput(t *testing.T) {
	var gotPath, gotHeader, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get(auth.APIKeyHeader)
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"items":[{"id":"repo1","name":"my-repo","git_url":"https://example.com/my-repo.git"}]}`))
	}))
	defer srv.Close()

	handler := listRepos(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.ListReposInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/collections/repositories/records" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotHeader != "wireops_sk_test" {
		t.Fatalf("expected API key forwarded, got %q", gotHeader)
	}
	if !strings.Contains(gotQuery, "perPage=50") {
		t.Fatalf("expected default perPage=50, got query %q", gotQuery)
	}
	if out == nil {
		t.Fatal("expected non-nil output")
	}
}

func TestListReposMissingAPIKey(t *testing.T) {
	handler := listRepos(client.New("http://unused"))
	_, _, err := handler(context.Background(), nil, models.ListReposInput{})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
	}
}

func TestListStacksMissingAPIKey(t *testing.T) {
	handler := listStacks(client.New("http://unused"))
	_, _, err := handler(context.Background(), nil, models.ListStacksInput{})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
	}
}

func TestGetStackStatusPathEscapesID(t *testing.T) {
	var gotEscapedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEscapedPath = r.URL.EscapedPath()
		w.Write([]byte(`{"id":"a/b"}`))
	}))
	defer srv.Close()

	handler := getStackStatus(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "a/b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// EscapedPath() reflects the wire form of the request, so this directly
	// confirms the stack id was sent as the single encoded segment "a%2Fb"
	// rather than as a literal "/" that would split into two path segments.
	if gotEscapedPath != "/api/collections/stacks/records/a%2Fb" {
		t.Fatalf("expected escaped stack id in path, got %q", gotEscapedPath)
	}
}

func TestGetStackStatusAddsNoticeWhenRenderOverridesActive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"stack1","render_overrides":{"web":{"image":"nginx:1.28"}}}`))
	}))
	defer srv.Close()

	handler := getStackStatus(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map output, got %T", out)
	}
	if result["_notice"] != renderOverridesNotice {
		t.Fatalf("expected render overrides notice, got %v", result["_notice"])
	}
}

func TestGetStackStatusNoNoticeWithoutRenderOverrides(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"stack1"}`))
	}))
	defer srv.Close()

	handler := getStackStatus(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map output, got %T", out)
	}
	if _, ok := result["_notice"]; ok {
		t.Fatalf("expected no notice when render_overrides is absent, got %v", result["_notice"])
	}
}

func TestGetSyncLogsFiltersByStackID(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	handler := getSyncLogs(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.SyncLogsInput{StackID: "stack123", Limit: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(gotQuery, "filter=stack") || !strings.Contains(gotQuery, "stack123") {
		t.Fatalf("expected filter on stack id, got query %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "perPage=5") {
		t.Fatalf("expected requested limit honored, got query %q", gotQuery)
	}
}

func TestGetStackServicesPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`[{"service_name":"web"}]`))
	}))
	defer srv.Close()

	handler := getStackServices(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/stacks/stack1/services" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	obj, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object output, got %T", out)
	}
	services, ok := obj["services"].([]interface{})
	if !ok || len(services) != 1 {
		t.Fatalf("expected services array to be preserved, got %#v", obj["services"])
	}
}

func TestGetStackResourcesPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"volumes":[],"networks":[]}`))
	}))
	defer srv.Close()

	handler := getStackResources(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/stacks/stack1/resources" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}

func TestGetStackComposePath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"content":"services: {}","filename":"v1.yml"}`))
	}))
	defer srv.Close()

	handler := getStackCompose(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/stacks/stack1/compose" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}

func TestGetStackRevisionPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"content":"services: {}","filename":"v34.yml"}`))
	}))
	defer srv.Close()

	handler := getStackRevision(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.StackRevisionInput{StackID: "stack1", Version: 34})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/stacks/stack1/revisions/34" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}

func TestGetStackRevisionMissingAPIKey(t *testing.T) {
	handler := getStackRevision(client.New("http://unused"))
	_, _, err := handler(context.Background(), nil, models.StackRevisionInput{StackID: "stack1", Version: 1})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
	}
}

func TestGetContainerStatsPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"cpu_percent":1.2}`))
	}))
	defer srv.Close()

	handler := getContainerStats(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ContainerStatsInput{StackID: "stack1", ContainerID: "web-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/stacks/stack1/container/web-1/stats" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}

func TestGetContainerStatsMissingAPIKey(t *testing.T) {
	handler := getContainerStats(client.New("http://unused"))
	_, _, err := handler(context.Background(), nil, models.ContainerStatsInput{StackID: "stack1", ContainerID: "web-1"})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
	}
}

func TestListAuditLogsDefaultsPagination(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"page":1,"items":[]}`))
	}))
	defer srv.Close()

	handler := listAuditLogs(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.AuditLogsInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/audit-logs" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if !strings.Contains(gotQuery, "page=1") || !strings.Contains(gotQuery, "perPage=25") {
		t.Fatalf("expected default pagination, got query %q", gotQuery)
	}
}

func TestListAuditLogsForwardsFilters(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"page":2,"items":[]}`))
	}))
	defer srv.Close()

	handler := listAuditLogs(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.AuditLogsInput{
		Page:    2,
		PerPage: 10,
		Action:  "stack.pause",
		ActorID: "user1",
		Status:  "success",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"page=2", "perPage=10", "action=stack.pause", "actor_id=user1", "status=success"} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("expected query to contain %q, got %q", want, gotQuery)
		}
	}
}

func TestListAuditLogsMissingAPIKey(t *testing.T) {
	handler := listAuditLogs(client.New("http://unused"))
	_, _, err := handler(context.Background(), nil, models.AuditLogsInput{})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
	}
}

func TestListIntegrationsPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`[{"slug":"traefik"}]`))
	}))
	defer srv.Close()

	handler := listIntegrations(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.ListIntegrationsInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/integrations" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	obj, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object output, got %T", out)
	}
	items, ok := obj["integrations"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("expected integrations array to be preserved, got %#v", obj["integrations"])
	}
}

func TestListIntegrationsMissingAPIKey(t *testing.T) {
	handler := listIntegrations(client.New("http://unused"))
	_, _, err := handler(context.Background(), nil, models.ListIntegrationsInput{})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
	}
}

func TestListOrphansPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`[{"dir_name":"old-repo"}]`))
	}))
	defer srv.Close()

	handler := listOrphans(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.ListOrphansInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/orphans" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	obj, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object output, got %T", out)
	}
	items, ok := obj["orphans"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("expected orphans array to be preserved, got %#v", obj["orphans"])
	}
}

func TestListOrphansMissingAPIKey(t *testing.T) {
	handler := listOrphans(client.New("http://unused"))
	_, _, err := handler(context.Background(), nil, models.ListOrphansInput{})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
	}
}

func TestGetSystemInfoPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"version":"1.0.0","disk_usage":123}`))
	}))
	defer srv.Close()

	handler := getSystemInfo(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.GetSystemInfoInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/system/info" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}

func TestGetSystemInfoMissingAPIKey(t *testing.T) {
	handler := getSystemInfo(client.New("http://unused"))
	_, _, err := handler(context.Background(), nil, models.GetSystemInfoInput{})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
	}
}

func TestListRepoStackFilesPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`["docker-compose.yml"]`))
	}))
	defer srv.Close()

	handler := listRepoStackFiles(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.RepositoryIDInput{RepositoryID: "repo1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/repositories/repo1/stack-files" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	obj, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object output, got %T", out)
	}
	files, ok := obj["files"].([]interface{})
	if !ok || len(files) != 1 {
		t.Fatalf("expected files array to be preserved, got %#v", obj["files"])
	}
}

func TestListRepoJobFilesPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`["job.yaml"]`))
	}))
	defer srv.Close()

	handler := listRepoJobFiles(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.RepositoryIDInput{RepositoryID: "repo1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/repositories/repo1/job-files" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	obj, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object output, got %T", out)
	}
	files, ok := obj["files"].([]interface{})
	if !ok || len(files) != 1 {
		t.Fatalf("expected files array to be preserved, got %#v", obj["files"])
	}
}

func TestListSecretsCombinesAllSourcesMaskedOnly(t *testing.T) {
	var gotPaths []string
	var gotQueries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		gotQueries = append(gotQueries, r.URL.RawQuery)
		switch r.URL.Path {
		case "/api/collections/stack_env_vars/records":
			w.Write([]byte(`{"items":[{"key":"DB_PASSWORD","secret_provider":"vault"},{"key":"API_TOKEN","secret_provider":""}]}`))
		case "/api/collections/stack_global_env_vars/records":
			w.Write([]byte(`{"items":[{"expand":{"global_env_var":{"key":"SHARED_SECRET","secret":true,"secret_provider":"infisical"}}}]}`))
		case "/api/custom/stacks/stack1/sops-env-vars":
			w.Write([]byte(`{"keys":["SOPS_KEY"],"available":true,"source_file":"secrets.yaml"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	handler := listSecrets(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Every query must request "fields" so values never leave the server,
	// regardless of provider.
	for i, q := range gotQueries {
		if !strings.Contains(q, "fields=") && gotPaths[i] != "/api/custom/stacks/stack1/sops-env-vars" {
			t.Fatalf("expected fields= restriction on %s, got query %q", gotPaths[i], q)
		}
	}

	result, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map output, got %T", out)
	}

	stackSecrets, ok := result["stack_secrets"].([]secretSummary)
	if !ok || len(stackSecrets) != 2 {
		t.Fatalf("expected 2 stack secrets, got %v", result["stack_secrets"])
	}
	globalSecrets, ok := result["global_secrets"].([]secretSummary)
	if !ok || len(globalSecrets) != 1 || globalSecrets[0].Key != "SHARED_SECRET" {
		t.Fatalf("expected 1 global secret SHARED_SECRET, got %v", result["global_secrets"])
	}
	sopsSecrets, ok := result["sops_secrets"].(map[string]interface{})
	if !ok || sopsSecrets["available"] != true {
		t.Fatalf("expected sops secrets to be available, got %v", result["sops_secrets"])
	}
}

func TestListSecretsMissingAPIKey(t *testing.T) {
	handler := listSecrets(client.New("http://unused"))
	_, _, err := handler(context.Background(), nil, models.StackIDInput{StackID: "stack1"})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
	}
}

func TestGetStackRenderOverridesPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"overrides":{"web":{"image":"nginx:1.28"}},"git":{"web":{"image":"nginx:1.27"}}}`))
	}))
	defer srv.Close()

	handler := getStackRenderOverrides(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/stacks/stack1/render-overrides" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}

func TestDiffStackVersionFetchesBothRevisions(t *testing.T) {
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		switch r.URL.Path {
		case "/api/custom/stacks/stack1/revisions/33":
			w.Write([]byte(`{"content":"image: nginx:1.27","filename":"v33.yml"}`))
		case "/api/custom/stacks/stack1/revisions/34":
			w.Write([]byte(`{"content":"image: nginx:1.28","filename":"v34.yml"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	handler := diffStackVersion(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.DiffStackVersionInput{StackID: "stack1", VersionA: 33, VersionB: 34})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gotPaths) != 2 || gotPaths[0] != "/api/custom/stacks/stack1/revisions/33" || gotPaths[1] != "/api/custom/stacks/stack1/revisions/34" {
		t.Fatalf("unexpected requested paths: %v", gotPaths)
	}
	result, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map output, got %T", out)
	}
	versionA, ok := result["version_a"].(map[string]interface{})
	if !ok || versionA["filename"] != "v33.yml" {
		t.Fatalf("unexpected version_a: %v", result["version_a"])
	}
	versionB, ok := result["version_b"].(map[string]interface{})
	if !ok || versionB["filename"] != "v34.yml" {
		t.Fatalf("unexpected version_b: %v", result["version_b"])
	}
}

func TestDiffStackVersionPropagatesFetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"revision not found"}`))
	}))
	defer srv.Close()

	handler := diffStackVersion(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.DiffStackVersionInput{StackID: "stack1", VersionA: 99, VersionB: 100})
	if err == nil {
		t.Fatal("expected error when a revision fetch fails")
	}
}

func TestGetContainerLogsPathAndTailQuery(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"logs":"line1\nline2"}`))
	}))
	defer srv.Close()

	handler := getContainerLogs(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ContainerLogsInput{StackID: "stack1", ContainerID: "web", Tail: "50"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/stacks/stack1/container/web/logs" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotQuery != "tail=50" {
		t.Fatalf("expected tail query forwarded, got %q", gotQuery)
	}
}

func TestGetContainerLogsOmitsTailQueryWhenUnset(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"logs":""}`))
	}))
	defer srv.Close()

	handler := getContainerLogs(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ContainerLogsInput{StackID: "stack1", ContainerID: "web"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotQuery != "" {
		t.Fatalf("expected no query params when tail unset, got %q", gotQuery)
	}
}

func TestListJobsPath(t *testing.T) {
	var gotPath, gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get(auth.APIKeyHeader)
		w.Write([]byte(`[{"id":"job1"}]`))
	}))
	defer srv.Close()

	handler := listJobs(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.ListJobsInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/jobs" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotHeader != "wireops_sk_test" {
		t.Fatalf("expected API key forwarded, got %q", gotHeader)
	}
	obj, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object output, got %T", out)
	}
	jobs, ok := obj["jobs"].([]interface{})
	if !ok || len(jobs) != 1 {
		t.Fatalf("expected jobs array to be preserved, got %#v", obj["jobs"])
	}
}

func TestListJobsMissingAPIKey(t *testing.T) {
	handler := listJobs(client.New("http://unused"))
	_, _, err := handler(context.Background(), nil, models.ListJobsInput{})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
	}
}

func TestGetJobDefinitionPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	handler := getJobDefinition(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.JobIDInput{JobID: "job1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/jobs/job1/definition" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}

func TestGetRepoCommitsPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`[{"sha":"abc123"}]`))
	}))
	defer srv.Close()

	handler := getRepoCommits(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.RepositoryIDInput{RepositoryID: "repo1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/repositories/repo1/commits" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	obj, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object output, got %T", out)
	}
	commits, ok := obj["commits"].([]interface{})
	if !ok || len(commits) != 1 {
		t.Fatalf("expected commits array to be preserved, got %#v", obj["commits"])
	}
}

func TestGetStackIntegrationActionsPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	handler := getStackIntegrationActions(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/stacks/stack1/integration-actions" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}

func TestListWorkersPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`[{"id":"worker1"}]`))
	}))
	defer srv.Close()

	handler := listWorkers(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.ListWorkersInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/workers" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	obj, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object output, got %T", out)
	}
	workers, ok := obj["workers"].([]interface{})
	if !ok || len(workers) != 1 {
		t.Fatalf("expected workers array to be preserved, got %#v", obj["workers"])
	}
}

func TestListWorkersMissingAPIKey(t *testing.T) {
	handler := listWorkers(client.New("http://unused"))
	_, _, err := handler(context.Background(), nil, models.ListWorkersInput{})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
	}
}

func TestGetWorkerMetricsPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	handler := getWorkerMetrics(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.WorkerIDInput{WorkerID: "worker1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/workers/worker1/metrics" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}

func TestGenerateWireopsYAMLValidInput(t *testing.T) {
	handler := generateWireopsYAML()
	_, out, err := handler(context.Background(), nil, models.GenerateWireopsYAMLInput{
		Name:       "my-stack",
		Timeout:    "5m",
		WorkerTags: []string{"node", "local"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	file, ok := out.(generatedFile)
	if !ok {
		t.Fatalf("expected generatedFile, got %T", out)
	}
	if file.Filename != "wireops.yaml" {
		t.Fatalf("unexpected filename: %s", file.Filename)
	}
	if !strings.Contains(file.Content, "name: my-stack") || !strings.Contains(file.Content, "version: wireops.v1") {
		t.Fatalf("expected generated content to include name/version, got: %s", file.Content)
	}
}

func TestGenerateWireopsYAMLMissingNameFails(t *testing.T) {
	handler := generateWireopsYAML()
	_, _, err := handler(context.Background(), nil, models.GenerateWireopsYAMLInput{})
	if err == nil {
		t.Fatal("expected error when name is missing")
	}
}

func TestGenerateJobYAMLValidInput(t *testing.T) {
	handler := generateJobYAML()
	_, out, err := handler(context.Background(), nil, models.GenerateJobYAMLInput{
		Name:        "cleanup",
		Description: "cleans stuff up",
		Cron:        "0 * * * *",
		Image:       "docker",
		CPU:         "1",
		Memory:      "512mb",
		ResTimeout:  "30s",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	file, ok := out.(generatedFile)
	if !ok {
		t.Fatalf("expected generatedFile, got %T", out)
	}
	if file.Filename != "job.yaml" {
		t.Fatalf("unexpected filename: %s", file.Filename)
	}
	if !strings.Contains(file.Content, "name: cleanup") || !strings.Contains(file.Content, "cron: 0 * * * *") {
		t.Fatalf("expected generated content to include name/cron, got: %s", file.Content)
	}
}

func TestGenerateJobYAMLMissingRequiredFieldsFails(t *testing.T) {
	handler := generateJobYAML()
	_, _, err := handler(context.Background(), nil, models.GenerateJobYAMLInput{Name: "cleanup"})
	if err == nil {
		t.Fatal("expected error when required fields are missing")
	}
}

func TestScaffoldStackValidInputNoWorker(t *testing.T) {
	handler := scaffoldStack(client.New("http://unused"))
	_, out, err := handler(context.Background(), nil, models.ScaffoldStackInput{
		Name: "my-stack",
		Services: []models.ComposeServiceInput{
			{Name: "web", Image: "nginx:1.27", Ports: []string{"80:80"}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, ok := out.(scaffoldStackOutput)
	if !ok {
		t.Fatalf("expected scaffoldStackOutput, got %T", out)
	}
	if result.Wireops.Filename != "wireops.yaml" || !strings.Contains(result.Wireops.Content, "name: my-stack") {
		t.Fatalf("unexpected wireops file: %+v", result.Wireops)
	}
	if result.Compose.Filename != "docker-compose.yml" || !strings.Contains(result.Compose.Content, "nginx:1.27") {
		t.Fatalf("unexpected compose file: %+v", result.Compose)
	}
}

func TestComposeConfigFromDeclaresTopLevelVolumesAndNetworks(t *testing.T) {
	config, err := composeConfigFrom([]models.ComposeServiceInput{
		{
			Name:     "web",
			Image:    "nginx:1.27",
			Volumes:  []string{"data:/var/data", "/host/path:/etc/config", "cache:/var/cache"},
			Networks: []string{"frontend", "backend"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	volumes, ok := config["volumes"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected top-level volumes map, got %T", config["volumes"])
	}
	if _, ok := volumes["data"]; !ok {
		t.Fatalf("expected named volume %q declared at top level, got %v", "data", volumes)
	}
	if _, ok := volumes["cache"]; !ok {
		t.Fatalf("expected named volume %q declared at top level, got %v", "cache", volumes)
	}
	if _, ok := volumes["/host/path"]; ok {
		t.Fatalf("host bind-mount source should not be declared as a top-level volume, got %v", volumes)
	}

	networks, ok := config["networks"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected top-level networks map, got %T", config["networks"])
	}
	if _, ok := networks["frontend"]; !ok {
		t.Fatalf("expected network %q declared at top level, got %v", "frontend", networks)
	}
	if _, ok := networks["backend"]; !ok {
		t.Fatalf("expected network %q declared at top level, got %v", "backend", networks)
	}

	svcs, ok := config["services"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected services map, got %T", config["services"])
	}
	web, ok := svcs["web"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected web service map, got %T", svcs["web"])
	}
	if _, ok := web["volumes"]; !ok {
		t.Fatal("expected service-level volumes to still be present")
	}
	if _, ok := web["networks"]; !ok {
		t.Fatal("expected service-level networks to still be present")
	}
}

func TestScaffoldStackRequiresAtLeastOneService(t *testing.T) {
	handler := scaffoldStack(client.New("http://unused"))
	_, _, err := handler(context.Background(), nil, models.ScaffoldStackInput{Name: "my-stack"})
	if err == nil {
		t.Fatal("expected error when no services are given")
	}
}

func TestScaffoldStackWithWorkerIDSurfacesPolicyViolation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"inherit":true,"effective":{"enabled":true,"allowed_volumes":[],"allowed_networks":[],"allowed_images":["redis:*"],"allowed_cap_add":[],"allowed_devices":[],"allowed_security_opt":[],"prevent_latest_images":false,"block_host_volumes":false,"block_privileged":false,"block_host_network":false,"block_host_pid":false,"block_host_ipc":false,"block_docker_socket":false,"allow_render_overrides":false}}`))
	}))
	defer srv.Close()

	handler := scaffoldStack(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ScaffoldStackInput{
		Name:     "my-stack",
		WorkerID: "worker1",
		Services: []models.ComposeServiceInput{
			{Name: "web", Image: "nginx:1.27", Ports: []string{"80:80"}},
		},
	})
	if err == nil {
		t.Fatal("expected policy violation error: nginx image is not in the worker's allowed_images list")
	}
	if !strings.Contains(err.Error(), "policy") {
		t.Fatalf("expected error to mention policy violation, got: %v", err)
	}
}

func TestScaffoldStackWithWorkerIDAllowsCompliantCompose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"inherit":true,"effective":{"enabled":true,"allowed_volumes":[],"allowed_networks":[],"allowed_images":[],"allowed_cap_add":[],"allowed_devices":[],"allowed_security_opt":[],"prevent_latest_images":false,"block_host_volumes":false,"block_privileged":false,"block_host_network":false,"block_host_pid":false,"block_host_ipc":false,"block_docker_socket":false,"allow_render_overrides":false}}`))
	}))
	defer srv.Close()

	handler := scaffoldStack(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ScaffoldStackInput{
		Name:     "my-stack",
		WorkerID: "worker1",
		Services: []models.ComposeServiceInput{
			{Name: "web", Image: "nginx:1.27", Ports: []string{"80:80"}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error for policy-compliant compose: %v", err)
	}
}

func TestScaffoldStackWithWorkerIDRejectsDisallowedVolume(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"inherit":true,"effective":{"enabled":true,"allowed_volumes":["data"],"allowed_networks":[],"allowed_images":[],"allowed_cap_add":[],"allowed_devices":[],"allowed_security_opt":[],"prevent_latest_images":false,"block_host_volumes":false,"block_privileged":false,"block_host_network":false,"block_host_pid":false,"block_host_ipc":false,"block_docker_socket":false,"allow_render_overrides":false}}`))
	}))
	defer srv.Close()

	handler := scaffoldStack(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ScaffoldStackInput{
		Name:     "my-stack",
		WorkerID: "worker1",
		Services: []models.ComposeServiceInput{
			{Name: "web", Image: "nginx:1.27", Volumes: []string{"other-volume:/data"}},
		},
	})
	if err == nil {
		t.Fatal("expected policy violation error: volume not in worker's allowed_volumes list")
	}
	if !strings.Contains(err.Error(), "policy") {
		t.Fatalf("expected error to mention policy violation, got: %v", err)
	}
}

func TestScaffoldStackWithWorkerIDRejectsDisallowedNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"inherit":true,"effective":{"enabled":true,"allowed_volumes":[],"allowed_networks":["frontend"],"allowed_images":[],"allowed_cap_add":[],"allowed_devices":[],"allowed_security_opt":[],"prevent_latest_images":false,"block_host_volumes":false,"block_privileged":false,"block_host_network":false,"block_host_pid":false,"block_host_ipc":false,"block_docker_socket":false,"allow_render_overrides":false}}`))
	}))
	defer srv.Close()

	handler := scaffoldStack(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ScaffoldStackInput{
		Name:     "my-stack",
		WorkerID: "worker1",
		Services: []models.ComposeServiceInput{
			{Name: "web", Image: "nginx:1.27", Networks: []string{"backend"}},
		},
	})
	if err == nil {
		t.Fatal("expected policy violation error: network not in worker's allowed_networks list")
	}
	if !strings.Contains(err.Error(), "policy") {
		t.Fatalf("expected error to mention policy violation, got: %v", err)
	}
}

func TestScaffoldStackWithWorkerIDRejectsHostVolumeWhenBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"inherit":true,"effective":{"enabled":true,"allowed_volumes":[],"allowed_networks":[],"allowed_images":[],"allowed_cap_add":[],"allowed_devices":[],"allowed_security_opt":[],"prevent_latest_images":false,"block_host_volumes":true,"block_privileged":false,"block_host_network":false,"block_host_pid":false,"block_host_ipc":false,"block_docker_socket":false,"allow_render_overrides":false}}`))
	}))
	defer srv.Close()

	handler := scaffoldStack(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ScaffoldStackInput{
		Name:     "my-stack",
		WorkerID: "worker1",
		Services: []models.ComposeServiceInput{
			{Name: "web", Image: "nginx:1.27", Volumes: []string{"/host/path:/data"}},
		},
	})
	if err == nil {
		t.Fatal("expected policy violation error: host bind-mount blocked by worker policy")
	}
	if !strings.Contains(err.Error(), "policy") {
		t.Fatalf("expected error to mention policy violation, got: %v", err)
	}
}

func TestScaffoldStackMissingAPIKeyWithWorkerID(t *testing.T) {
	handler := scaffoldStack(client.New("http://unused"))
	_, _, err := handler(context.Background(), nil, models.ScaffoldStackInput{
		Name:     "my-stack",
		WorkerID: "worker1",
		Services: []models.ComposeServiceInput{
			{Name: "web", Image: "nginx:1.27"},
		},
	})
	if err == nil {
		t.Fatal("expected error when API key missing from context but worker_id was set")
	}
}
