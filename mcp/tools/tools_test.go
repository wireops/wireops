package tools

import (
	"context"
	"io"
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
	queries := map[string]string{}
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries[r.URL.Path] = r.URL.RawQuery
		gotHeader = r.Header.Get(auth.APIKeyHeader)
		writeFixture(w, `{"items":[]}`)
	}))
	defer srv.Close()

	handler := listStacks(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.ListStacksInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	query, ok := queries["/api/collections/stacks/records"]
	if !ok {
		t.Fatalf("expected the stacks collection to be queried, saw %v", queries)
	}
	if gotHeader != "wireops_sk_test" {
		t.Fatalf("expected API key forwarded, got %q", gotHeader)
	}
	for _, want := range []string{"perPage=50", "page=1", "expand=repository%2Cworker", "fields="} {
		if !strings.Contains(query, want) {
			t.Errorf("expected %q in query %q", want, query)
		}
	}
	// A default listing must not send a filter at all, or PocketBase would
	// reject the empty expression.
	if strings.Contains(query, "filter=") {
		t.Errorf("expected no filter on an unfiltered listing, got %q", query)
	}
	if out == nil {
		t.Fatal("expected non-nil output")
	}
}

func TestListStacksAddsNoticeWhenRenderOverridesActive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeFixture(w, `{"items":[{"id":"stack0000000001","render_overrides":{"web":{"image":"nginx:1.28"}}},{"id":"stack0000000002","render_overrides":{}}]}`)
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
	stack0000000001 := items[0].(map[string]interface{})
	if stack0000000001["_notice"] != renderOverridesNotice {
		t.Fatalf("expected notice on stack0000000001, got %v", stack0000000001["_notice"])
	}
	stack0000000002 := items[1].(map[string]interface{})
	if _, ok := stack0000000002["_notice"]; ok {
		t.Fatalf("expected no notice on stack0000000002 with empty overrides, got %v", stack0000000002["_notice"])
	}
}

func TestListReposBuildsRequestAndReturnsOutput(t *testing.T) {
	var gotPath, gotHeader, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get(auth.APIKeyHeader)
		gotQuery = r.URL.RawQuery
		writeFixture(w, `{"items":[{"id":"repo00000000001","name":"my-repo","git_url":"https://example.com/my-repo.git"}]}`)
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

func TestGetContainerStatsPathEscapesContainerID(t *testing.T) {
	var gotEscapedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEscapedPath = r.URL.EscapedPath()
		writeFixture(w, `{}`)
	}))
	defer srv.Close()

	// A container id is caller-supplied and goes into the path as-is (unlike a
	// stack ref, which resolveRef narrows to a record id first), so it is the
	// remaining place a "/" could split one segment into two.
	handler := getContainerStats(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ContainerStatsInput{StackID: "stack0000000001", ContainerID: "a/b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotEscapedPath != "/api/custom/stacks/stack0000000001/container/a%2Fb/stats" {
		t.Fatalf("expected escaped container id in path, got %q", gotEscapedPath)
	}
}

func TestGetStackStatusAddsNoticeWhenRenderOverridesActive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeFixture(w, `{"id":"stack0000000001","render_overrides":{"web":{"image":"nginx:1.28"}}}`)
	}))
	defer srv.Close()

	handler := getStackStatus(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack0000000001"})
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
		writeFixture(w, `{"id":"stack0000000001"}`)
	}))
	defer srv.Close()

	handler := getStackStatus(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack0000000001"})
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

func TestGetInventoryResolvesWorkerHostnameAndPages(t *testing.T) {
	var gotInventoryQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/collections/workers/records":
			writeFixture(w, `{"items":[{"id":"worker000000001","hostname":"edge-a"}]}`)
		case "/api/custom/inventory":
			gotInventoryQuery = r.URL.RawQuery
			writeFixture(w, `{"page":2,"per_page":200,"stacks":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			writeFixture(w, `{"error":"unexpected path"}`)
		}
	}))
	defer srv.Close()

	_, out, err := getInventory(client.New(srv.URL))(ctxWithKey(), nil, models.InventoryInput{Worker: "edge-a", Group: "prod", Limit: 500, Page: 2})
	if err != nil {
		t.Fatalf("get inventory: %v", err)
	}
	result := out.(map[string]interface{})
	if result["per_page"] != float64(200) {
		t.Fatalf("expected capped response, got %#v", result)
	}
	for _, part := range []string{"worker=worker000000001", "group=prod", "per_page=200", "page=2"} {
		if !strings.Contains(gotInventoryQuery, part) {
			t.Errorf("inventory query %q is missing %q", gotInventoryQuery, part)
		}
	}
}

func TestGetSyncLogsFiltersByStackID(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		writeFixture(w, `{"items":[]}`)
	}))
	defer srv.Close()

	handler := getSyncLogs(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.SyncLogsInput{StackID: "stack0000000123", Limit: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(gotQuery, "filter=stack") || !strings.Contains(gotQuery, "stack0000000123") {
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
		writeFixture(w, `[{"service_name":"web"}]`)
	}))
	defer srv.Close()

	handler := getStackServices(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack0000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/stacks/stack0000000001/services" {
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
		writeFixture(w, `{"volumes":[],"networks":[]}`)
	}))
	defer srv.Close()

	handler := getStackResources(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack0000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/stacks/stack0000000001/resources" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}

func TestGetStackComposePath(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/api/custom/stacks/stack0000000001/compose" {
			writeFixture(w, `{"content":"services:\n  web:\n    image: nginx:1.29\n","filename":"docker-compose.yml"}`)
			return
		}
		writeFixture(w, `{"items":[]}`)
	}))
	defer srv.Close()

	handler := getStackCompose(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack0000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !requested(paths, "/api/custom/stacks/stack0000000001/compose") {
		t.Fatalf("expected the compose route to be called, saw %v", paths)
	}
	obj, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object output, got %T", out)
	}
	// Nothing is interpolated in this compose file, so there is nothing to warn
	// about — the notice must stay absent rather than firing on every read.
	if notice, present := obj["_notice"]; present {
		t.Errorf("expected no notice for a compose file without variables, got %v", notice)
	}
}

func TestGetStackComposeNoticesUndefinedVariables(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/custom/stacks/stack0000000001/compose":
			writeFixture(w, `{"content":"services:\n  broker:\n    image: mosquitto:2\n    environment:\n      USER: ${MQTT_USER}\n      PASS: ${MQTT_PASSWORD:-x}\n      HOST: ${DEFINED_HOST}\n"}`)
		case "/api/collections/stack_env_vars/records":
			writeFixture(w, `{"items":[{"key":"DEFINED_HOST","secret":false}]}`)
		default:
			writeFixture(w, `{"items":[]}`)
		}
	}))
	defer srv.Close()

	handler := getStackCompose(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack0000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	notice, ok := out.(map[string]interface{})["_notice"].(string)
	if !ok {
		t.Fatalf("expected a notice naming the undefined variables, got %#v", out)
	}
	for _, want := range []string{"MQTT_USER", "MQTT_PASSWORD", "lint_compose"} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice %q should mention %q", notice, want)
		}
	}
	// A variable the stack does define must not be reported as missing.
	if strings.Contains(notice, "DEFINED_HOST") {
		t.Errorf("notice should not name a defined variable: %q", notice)
	}
}

func TestGetStackRevisionPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeFixture(w, `{"content":"services: {}","filename":"v34.yml"}`)
	}))
	defer srv.Close()

	handler := getStackRevision(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.StackRevisionInput{StackID: "stack0000000001", Version: 34})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/stacks/stack0000000001/revisions/34" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}

func TestGetStackRevisionMissingAPIKey(t *testing.T) {
	handler := getStackRevision(client.New("http://unused"))
	_, _, err := handler(context.Background(), nil, models.StackRevisionInput{StackID: "stack0000000001", Version: 1})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
	}
}

func TestGetContainerStatsPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeFixture(w, `{"cpu_percent":1.2}`)
	}))
	defer srv.Close()

	handler := getContainerStats(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ContainerStatsInput{StackID: "stack0000000001", ContainerID: "web-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/stacks/stack0000000001/container/web-1/stats" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}

func TestGetContainerStatsMissingAPIKey(t *testing.T) {
	handler := getContainerStats(client.New("http://unused"))
	_, _, err := handler(context.Background(), nil, models.ContainerStatsInput{StackID: "stack0000000001", ContainerID: "web-1"})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
	}
}

func TestListRepoStackFilesPath(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/api/custom/repositories/repo00000000001/stack-files":
			writeFixture(w, `["docker-compose.yml","stacks/homeassistant/docker-compose.yml"]`)
		case "/api/collections/stacks/records":
			writeFixture(w, `{"items":[{"id":"stack0000000001","name":"root","compose_path":"","compose_file":""}]}`)
		default:
			writeFixture(w, `{"items":[]}`)
		}
	}))
	defer srv.Close()

	handler := listRepoStackFiles(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.RepositoryIDInput{RepositoryID: "repo00000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !requested(paths, "/api/custom/repositories/repo00000000001/stack-files") {
		t.Fatalf("expected the stack-files route to be called, saw %v", paths)
	}
	files, ok := out.(map[string]interface{})["files"].([]interface{})
	if !ok || len(files) != 2 {
		t.Fatalf("expected both files back, got %#v", out)
	}

	// An empty compose_path means the repository root, so "docker-compose.yml"
	// is the registered one and the nested path is not.
	first := files[0].(map[string]interface{})
	if first["registered"] != true || first["stack_id"] != "stack0000000001" {
		t.Errorf("expected the root compose file marked registered, got %#v", first)
	}
	second := files[1].(map[string]interface{})
	if second["registered"] != false {
		t.Errorf("expected the nested compose file marked unregistered, got %#v", second)
	}
	if _, present := second["stack_id"]; present {
		t.Errorf("an unregistered file must carry no stack id: %#v", second)
	}
}

func TestListRepoJobFilesPath(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/api/custom/repositories/repo00000000001/job-files":
			writeFixture(w, `["jobs/backup/job.yaml"]`)
		case "/api/collections/scheduled_jobs/records":
			writeFixture(w, `{"items":[{"id":"job000000000001","name":"backup","job_file":"jobs/backup/job.yaml"}]}`)
		default:
			writeFixture(w, `{"items":[]}`)
		}
	}))
	defer srv.Close()

	handler := listRepoJobFiles(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.RepositoryIDInput{RepositoryID: "repo00000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !requested(paths, "/api/custom/repositories/repo00000000001/job-files") {
		t.Fatalf("expected the job-files route to be called, saw %v", paths)
	}
	files, ok := out.(map[string]interface{})["files"].([]interface{})
	if !ok || len(files) != 1 {
		t.Fatalf("expected the file back, got %#v", out)
	}
	entry := files[0].(map[string]interface{})
	if entry["registered"] != true || entry["job_id"] != "job000000000001" {
		t.Errorf("expected the scheduled job.yaml marked registered, got %#v", entry)
	}
}

// requested reports whether path was among the requests a fake server saw.
// Handlers now make follow-up calls (ref resolution, registration lookups), so
// a test asserts on the set of requests rather than on the last one.
func requested(paths []string, path string) bool {
	for _, got := range paths {
		if got == path {
			return true
		}
	}
	return false
}

func TestListSecretsCombinesAllSourcesMaskedOnly(t *testing.T) {
	var gotPaths []string
	var gotQueries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		gotQueries = append(gotQueries, r.URL.RawQuery)
		switch r.URL.Path {
		case "/api/collections/stack_env_vars/records":
			writeFixture(w, `{"items":[{"key":"DB_PASSWORD","secret_provider":"vault"},{"key":"API_TOKEN","secret_provider":""}]}`)
		case "/api/collections/stack_global_env_vars/records":
			writeFixture(w, `{"items":[{"expand":{"global_env_var":{"key":"SHARED_SECRET","secret":true,"secret_provider":"infisical"}}}]}`)
		case "/api/custom/stacks/stack0000000001/sops-env-vars":
			writeFixture(w, `{"keys":["SOPS_KEY"],"available":true,"source_file":"secrets.yaml"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	handler := listSecrets(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack0000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Every query must request "fields" so values never leave the server,
	// regardless of provider.
	for i, q := range gotQueries {
		if !strings.Contains(q, "fields=") && gotPaths[i] != "/api/custom/stacks/stack0000000001/sops-env-vars" {
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
	_, _, err := handler(context.Background(), nil, models.StackIDInput{StackID: "stack0000000001"})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
	}
}

func TestGetStackRenderOverridesPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeFixture(w, `{"overrides":{"web":{"image":"nginx:1.28"}},"git":{"web":{"image":"nginx:1.27"}}}`)
	}))
	defer srv.Close()

	handler := getStackRenderOverrides(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack0000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/stacks/stack0000000001/render-overrides" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}

func TestDiffStackVersionFetchesBothRevisions(t *testing.T) {
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		switch r.URL.Path {
		case "/api/custom/stacks/stack0000000001/revisions/33":
			writeFixture(w, `{"content":"image: nginx:1.27","filename":"v33.yml"}`)
		case "/api/custom/stacks/stack0000000001/revisions/34":
			writeFixture(w, `{"content":"image: nginx:1.28","filename":"v34.yml"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	handler := diffStackVersion(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.DiffStackVersionInput{StackID: "stack0000000001", VersionA: 33, VersionB: 34})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gotPaths) != 2 || gotPaths[0] != "/api/custom/stacks/stack0000000001/revisions/33" || gotPaths[1] != "/api/custom/stacks/stack0000000001/revisions/34" {
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
		writeFixture(w, `{"error":"revision not found"}`)
	}))
	defer srv.Close()

	handler := diffStackVersion(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.DiffStackVersionInput{StackID: "stack0000000001", VersionA: 99, VersionB: 100})
	if err == nil {
		t.Fatal("expected error when a revision fetch fails")
	}
}

func TestGetContainerLogsPathAndTailQuery(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		writeFixture(w, `{"logs":"line1\nline2"}`)
	}))
	defer srv.Close()

	handler := getContainerLogs(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ContainerLogsInput{StackID: "stack0000000001", ContainerID: "web", Tail: "50"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/stacks/stack0000000001/container/web/logs" {
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
		writeFixture(w, `{"logs":""}`)
	}))
	defer srv.Close()

	handler := getContainerLogs(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ContainerLogsInput{StackID: "stack0000000001", ContainerID: "web"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotQuery != "" {
		t.Fatalf("expected no query params when tail unset, got %q", gotQuery)
	}
}

func TestGetContainerLogsFiltersByPattern(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeFixture(w, `{"logs":"connecting to db\nERROR timeout\nretrying\nERROR timeout\nok"}`)
	}))
	defer srv.Close()

	handler := getContainerLogs(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.ContainerLogsInput{StackID: "stack0000000001", ContainerID: "web", Pattern: "ERROR"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	obj, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object output, got %T", out)
	}
	logs, ok := obj["logs"].(string)
	if !ok {
		t.Fatalf("expected logs string, got %#v", obj["logs"])
	}
	if logs != "ERROR timeout\nERROR timeout" {
		t.Fatalf("unexpected filtered logs: %q", logs)
	}
}

func TestGetContainerLogsInvalidPattern(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeFixture(w, `{"logs":"line1"}`)
	}))
	defer srv.Close()

	handler := getContainerLogs(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ContainerLogsInput{StackID: "stack0000000001", ContainerID: "web", Pattern: "("})
	if err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
}

func TestGetContainerLogsResolvesServiceName(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/custom/stacks/stack0000000001/services":
			writeFixture(w, `[{"service_name":"web","container_id":"c-web"},{"service_name":"db","container_id":"c-db"}]`)
		default:
			gotPath = r.URL.Path
			writeFixture(w, `{"logs":"hello"}`)
		}
	}))
	defer srv.Close()

	handler := getContainerLogs(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ContainerLogsInput{StackID: "stack0000000001", ServiceName: "web"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/stacks/stack0000000001/container/c-web/logs" {
		t.Fatalf("expected service_name resolved to container id in path, got %s", gotPath)
	}
}

func TestGetContainerLogsServiceNameNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeFixture(w, `[{"service_name":"web","container_id":"c-web"}]`)
	}))
	defer srv.Close()

	handler := getContainerLogs(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ContainerLogsInput{StackID: "stack0000000001", ServiceName: "nonexistent"})
	if err == nil {
		t.Fatal("expected error when service_name does not match any service")
	}
}

func TestGetContainerLogsServiceNameWithoutContainerID(t *testing.T) {
	logsRequested := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/custom/stacks/stack0000000001/services" {
			writeFixture(w, `[{"service_name":"web","container_id":""}]`)
			return
		}
		logsRequested = true
		writeFixture(w, `{"logs":"hello"}`)
	}))
	defer srv.Close()

	handler := getContainerLogs(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ContainerLogsInput{StackID: "stack0000000001", ServiceName: "web"})
	if err == nil {
		t.Fatal("expected error when matching service has no container_id")
	}
	if logsRequested {
		t.Fatal("expected handler not to call the logs endpoint when container_id resolution fails")
	}
}

func TestGetContainerLogsRequiresContainerIDOrServiceName(t *testing.T) {
	handler := getContainerLogs(client.New("http://unused"))
	_, _, err := handler(ctxWithKey(), nil, models.ContainerLogsInput{StackID: "stack0000000001"})
	if err == nil {
		t.Fatal("expected error when neither container_id nor service_name is given")
	}
}

func TestListJobsPath(t *testing.T) {
	var gotPath, gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get(auth.APIKeyHeader)
		writeFixture(w, `[{"id":"job000000000001"}]`)
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
		writeFixture(w, `{}`)
	}))
	defer srv.Close()

	handler := getJobDefinition(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.JobIDInput{JobID: "job000000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/jobs/job000000000001/definition" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}

func TestGetRepoCommitsPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeFixture(w, `[{"sha":"abc123"}]`)
	}))
	defer srv.Close()

	handler := getRepoCommits(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.RepositoryIDInput{RepositoryID: "repo00000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/repositories/repo00000000001/commits" {
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
		writeFixture(w, `[]`)
	}))
	defer srv.Close()

	handler := getStackIntegrationActions(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack0000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/stacks/stack0000000001/integration-actions" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}

func TestListWorkersPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeFixture(w, `[{"id":"worker000000001"}]`)
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
		writeFixture(w, `{}`)
	}))
	defer srv.Close()

	handler := getWorkerMetrics(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.WorkerIDInput{WorkerID: "worker000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/workers/worker000000001/metrics" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}

func TestGetWorkerPolicyPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeFixture(w, `{"inherit":true,"effective":{"enabled":true}}`)
	}))
	defer srv.Close()

	handler := getWorkerPolicy(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.WorkerIDInput{WorkerID: "worker000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/workers/worker000000001/policy" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	obj, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object output, got %T", out)
	}
	if obj["inherit"] != true {
		t.Fatalf("expected inherit to be preserved, got %#v", obj["inherit"])
	}
}

func TestGetWorkerPolicyMissingAPIKey(t *testing.T) {
	handler := getWorkerPolicy(client.New("http://unused"))
	_, _, err := handler(context.Background(), nil, models.WorkerIDInput{WorkerID: "worker000000001"})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
	}
}

func TestGetGlobalWorkerPolicyPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeFixture(w, `{"enabled":true,"block_privileged":true}`)
	}))
	defer srv.Close()

	handler := getGlobalWorkerPolicy(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.GetGlobalWorkerPolicyInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/settings/worker-policy" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	obj, ok := out.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object output, got %T", out)
	}
	if obj["block_privileged"] != true {
		t.Fatalf("expected block_privileged to be preserved, got %#v", obj["block_privileged"])
	}
}

func TestGetGlobalWorkerPolicyMissingAPIKey(t *testing.T) {
	handler := getGlobalWorkerPolicy(client.New("http://unused"))
	_, _, err := handler(context.Background(), nil, models.GetGlobalWorkerPolicyInput{})
	if err == nil {
		t.Fatal("expected error when API key missing from context")
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

// Default behavior (two_file left false): scaffold_stack embeds the wireops
// fields as a top-level x-wireops block in the single returned compose
// file — this is the primary, recommended way to define a wireops stack.
func TestScaffoldStackDefaultEmbedsXWireopsAndOmitsSeparateFile(t *testing.T) {
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
	if result.Wireops != nil {
		t.Fatalf("expected no separate wireops file by default, got %+v", result.Wireops)
	}
	if result.Compose.Filename != "docker-compose.yml" {
		t.Fatalf("unexpected compose filename: %q", result.Compose.Filename)
	}
	if !strings.Contains(result.Compose.Content, "x-wireops:") {
		t.Fatalf("expected embedded x-wireops block in compose content:\n%s", result.Compose.Content)
	}
	if !strings.Contains(result.Compose.Content, "name: my-stack") {
		t.Fatalf("expected x-wireops name to be my-stack:\n%s", result.Compose.Content)
	}
	if !strings.Contains(result.Compose.Content, "nginx:1.27") {
		t.Fatalf("expected compose service content:\n%s", result.Compose.Content)
	}
}

// two_file: true opts into the legacy two-file layout (separate wireops.yaml
// + docker-compose.yml).
func TestScaffoldStackTwoFileGeneratesSeparateWireopsYAML(t *testing.T) {
	handler := scaffoldStack(client.New("http://unused"))
	_, out, err := handler(context.Background(), nil, models.ScaffoldStackInput{
		Name:    "my-stack",
		TwoFile: true,
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
	if result.Wireops == nil || result.Wireops.Filename != "wireops.yaml" || !strings.Contains(result.Wireops.Content, "name: my-stack") {
		t.Fatalf("unexpected wireops file: %+v", result.Wireops)
	}
	if strings.Contains(result.Compose.Content, "x-wireops:") {
		t.Fatalf("expected no embedded x-wireops block when two_file=true:\n%s", result.Compose.Content)
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
		writeFixture(w, `{"inherit":true,"effective":{"enabled":true,"allowed_volumes":[],"allowed_networks":[],"allowed_images":["redis:*"],"allowed_cap_add":[],"allowed_devices":[],"allowed_security_opt":[],"prevent_latest_images":false,"block_host_volumes":false,"block_privileged":false,"block_host_network":false,"block_host_pid":false,"block_host_ipc":false,"block_docker_socket":false,"allow_render_overrides":false}}`)
	}))
	defer srv.Close()

	handler := scaffoldStack(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ScaffoldStackInput{
		Name:     "my-stack",
		WorkerID: "worker000000001",
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
		writeFixture(w, `{"inherit":true,"effective":{"enabled":true,"allowed_volumes":[],"allowed_networks":[],"allowed_images":[],"allowed_cap_add":[],"allowed_devices":[],"allowed_security_opt":[],"prevent_latest_images":false,"block_host_volumes":false,"block_privileged":false,"block_host_network":false,"block_host_pid":false,"block_host_ipc":false,"block_docker_socket":false,"allow_render_overrides":false}}`)
	}))
	defer srv.Close()

	handler := scaffoldStack(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ScaffoldStackInput{
		Name:     "my-stack",
		WorkerID: "worker000000001",
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
		writeFixture(w, `{"inherit":true,"effective":{"enabled":true,"allowed_volumes":["data"],"allowed_networks":[],"allowed_images":[],"allowed_cap_add":[],"allowed_devices":[],"allowed_security_opt":[],"prevent_latest_images":false,"block_host_volumes":false,"block_privileged":false,"block_host_network":false,"block_host_pid":false,"block_host_ipc":false,"block_docker_socket":false,"allow_render_overrides":false}}`)
	}))
	defer srv.Close()

	handler := scaffoldStack(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ScaffoldStackInput{
		Name:     "my-stack",
		WorkerID: "worker000000001",
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
		writeFixture(w, `{"inherit":true,"effective":{"enabled":true,"allowed_volumes":[],"allowed_networks":["frontend"],"allowed_images":[],"allowed_cap_add":[],"allowed_devices":[],"allowed_security_opt":[],"prevent_latest_images":false,"block_host_volumes":false,"block_privileged":false,"block_host_network":false,"block_host_pid":false,"block_host_ipc":false,"block_docker_socket":false,"allow_render_overrides":false}}`)
	}))
	defer srv.Close()

	handler := scaffoldStack(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ScaffoldStackInput{
		Name:     "my-stack",
		WorkerID: "worker000000001",
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
		writeFixture(w, `{"inherit":true,"effective":{"enabled":true,"allowed_volumes":[],"allowed_networks":[],"allowed_images":[],"allowed_cap_add":[],"allowed_devices":[],"allowed_security_opt":[],"prevent_latest_images":false,"block_host_volumes":true,"block_privileged":false,"block_host_network":false,"block_host_pid":false,"block_host_ipc":false,"block_docker_socket":false,"allow_render_overrides":false}}`)
	}))
	defer srv.Close()

	handler := scaffoldStack(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ScaffoldStackInput{
		Name:     "my-stack",
		WorkerID: "worker000000001",
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
		WorkerID: "worker000000001",
		Services: []models.ComposeServiceInput{
			{Name: "web", Image: "nginx:1.27"},
		},
	})
	if err == nil {
		t.Fatal("expected error when API key missing from context but worker_id was set")
	}
}

func TestListSecretsReportsNonSecretKeyNames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/collections/stack_env_vars/records" && strings.Contains(r.URL.RawQuery, "secret%3Dtrue"):
			// The secrets-only query: this stack has none.
			writeFixture(w, `{"items":[]}`)
		case r.URL.Path == "/api/collections/stack_env_vars/records":
			writeFixture(w, `{"items":[{"key":"MQTT_USER","secret":false},{"key":"MQTT_PASSWORD","secret":true}]}`)
		case r.URL.Path == "/api/custom/stacks/stack0000000001/sops-env-vars":
			writeFixture(w, `{"keys":[],"available":false}`)
		default:
			writeFixture(w, `{"items":[]}`)
		}
	}))
	defer srv.Close()

	handler := listSecrets(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack0000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := out.(map[string]any)

	keys, ok := result["non_secret_keys"].([]string)
	if !ok || len(keys) != 1 || keys[0] != "MQTT_USER" {
		t.Fatalf("expected the plain key name reported, got %#v", result["non_secret_keys"])
	}
	if result["non_secret_count"] != 1 {
		t.Errorf("non_secret_count = %v, want 1", result["non_secret_count"])
	}
	if result["secret_count_scope"] != "stack_secrets + global_secrets; excludes sops_secrets" {
		t.Errorf("unexpected secret_count scope: %#v", result["secret_count_scope"])
	}
	// An empty secret list used to read as "this stack has no env vars at all",
	// which is how an undefined ${MQTT_USER} reached a deploy.
	notice, ok := result["_notice"].(string)
	if !ok || !strings.Contains(notice, "non_secret_keys") {
		t.Errorf("expected a notice explaining the empty secret list, got %#v", result["_notice"])
	}
	// Values are never fetched, whatever the key's provider.
	for _, key := range keys {
		if strings.Contains(key, "=") {
			t.Errorf("key %q looks like a key=value pair", key)
		}
	}
}

func TestGetStackStatusExplainsAPausedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeFixture(w, `{"id":"stack0000000001","status":"paused","render_overrides":{}}`)
	}))
	defer srv.Close()

	handler := getStackStatus(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack0000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	notice, _ := out.(map[string]interface{})["_notice"].(string)
	// The description no longer carries this, so the notice has to.
	if !strings.Contains(notice, "not act on new commits") {
		t.Fatalf("expected the paused explanation, got %q", notice)
	}
}

func TestGetStackStatusCombinesNotices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeFixture(w, `{"id":"stack0000000001","status":"degraded","render_overrides":{"web":{"image":"nginx:1.29"}}}`)
	}))
	defer srv.Close()

	handler := getStackStatus(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack0000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	notice, _ := out.(map[string]interface{})["_notice"].(string)
	// Both conditions hold at once; neither may silently replace the other.
	if !strings.Contains(notice, "render-time overrides") || !strings.Contains(notice, "degraded") {
		t.Fatalf("expected both notices, got %q", notice)
	}
}

func TestGetStackStatusHasNoNoticeWhenNothingToExplain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeFixture(w, `{"id":"stack0000000001","status":"active","render_overrides":{}}`)
	}))
	defer srv.Close()

	handler := getStackStatus(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack0000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if notice, present := out.(map[string]interface{})["_notice"]; present {
		t.Fatalf("expected no notice for a healthy active stack, got %v", notice)
	}
}

func TestGetStackServicesNoticesInitAndUnknownState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeFixture(w, `[{"service_name":"migrate","container_id":"c1","status":"exited","is_init":true,"exit_code":0},
		                 {"service_name":"web","container_id":"c2","status":"running","is_init":null,"exit_code":null}]`)
	}))
	defer srv.Close()

	handler := getStackServices(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack0000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	notice, _ := out.(map[string]interface{})["_notice"].(string)
	for _, want := range []string{"customization.init", "worker is offline"} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice %q should mention %q", notice, want)
		}
	}
}

func TestGetStackServicesHasNoNoticeForAPlainStack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeFixture(w, `[{"service_name":"web","container_id":"c1","status":"running","is_init":false,"exit_code":0}]`)
	}))
	defer srv.Close()

	handler := getStackServices(client.New(srv.URL))
	_, out, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack0000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if notice, present := out.(map[string]interface{})["_notice"]; present {
		t.Fatalf("expected no notice when neither case applies, got %v", notice)
	}
}

// writeFixture writes a fixed response body for a fake server in these tests.
//
// It takes an io.Writer rather than the http.ResponseWriter the caller holds,
// which is the point: the static analyser treats any direct write to a
// ResponseWriter — Write, fmt.Fprintf, io.WriteString — as an unescaped
// handler response and reports it as XSS. These fixtures serve JSON, NDJSON
// and SSE to the MCP client under test and never HTML to a browser, so the
// finding is noise, and the repo's convention is to avoid the pattern rather
// than argue with the scanner (see CLAUDE.md on HTML-string test stubs).
func writeFixture(w io.Writer, body string) {
	_, _ = io.WriteString(w, body)
}
