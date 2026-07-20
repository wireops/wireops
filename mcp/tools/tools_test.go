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
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	handler := getStackServices(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "stack1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/stacks/stack1/services" {
		t.Fatalf("unexpected path: %s", gotPath)
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
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	handler := listJobs(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ListJobsInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/jobs" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotHeader != "wireops_sk_test" {
		t.Fatalf("expected API key forwarded, got %q", gotHeader)
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
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	handler := getRepoCommits(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.RepositoryIDInput{RepositoryID: "repo1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/repositories/repo1/commits" {
		t.Fatalf("unexpected path: %s", gotPath)
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
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	handler := listWorkers(client.New(srv.URL))
	_, _, err := handler(ctxWithKey(), nil, models.ListWorkersInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/custom/workers" {
		t.Fatalf("unexpected path: %s", gotPath)
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
