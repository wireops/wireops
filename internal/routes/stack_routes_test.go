package routes

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	_ "github.com/wireops/wireops/pb_migrations"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/protocol"
	wiresync "github.com/wireops/wireops/internal/sync"
	"github.com/wireops/wireops/internal/webhook"
)

const testWebhookSecretKeyEnv = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // 32 bytes

func setupTestWebhookApp(t *testing.T) (core.App, http.Handler) {
	t.Helper()
	t.Setenv("SECRET_KEY", testWebhookSecretKeyEnv)

	app := newSetupTestApp(t)

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:   app,
			Event: router.Event{Response: w, Request: req},
		}, nil
	})

	rr := routeRegistrar{
		r:         r,
		app:       app,
		scheduler: wiresync.NewScheduler(app, nil),
	}
	rr.registerStackTriggerRoutes()

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return app, mux
}

func createTestRepoWithBranch(t *testing.T, app core.App, branch string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("repositories")
	if err != nil {
		t.Fatalf("find repositories collection: %v", err)
	}
	repo := core.NewRecord(col)
	repo.Set("name", "test-repo")
	repo.Set("git_url", "https://example.com/test-repo.git")
	repo.Set("branch", branch)
	if err := app.Save(repo); err != nil {
		t.Fatalf("create test repository: %v", err)
	}
	return repo
}

func createTestStackWithWebhookSecret(t *testing.T, app core.App, repoID, plainSecret string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		t.Fatalf("find stacks collection: %v", err)
	}
	stack := core.NewRecord(col)
	stack.Set("name", "test-stack")
	stack.Set("repository", repoID)

	if plainSecret != "" {
		encrypted, err := crypto.Encrypt([]byte(plainSecret), crypto.NormalizeSecretKey(testWebhookSecretKeyEnv))
		if err != nil {
			t.Fatalf("encrypt test webhook secret: %v", err)
		}
		stack.Set("webhook_secret", encrypted)
	}

	if err := app.Save(stack); err != nil {
		t.Fatalf("create test stack: %v", err)
	}
	return stack
}

func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func postWebhook(t *testing.T, mux http.Handler, stackID string, body []byte, sigHeader string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/custom/webhook/"+stackID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if sigHeader != "" {
		req.Header.Set(webhook.GitHubSignatureHeader, sigHeader)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestWebhookRejectsWhenSecretNotConfigured(t *testing.T) {
	app, mux := setupTestWebhookApp(t)
	repo := createTestRepoWithBranch(t, app, "main")
	stack := createTestStackWithWebhookSecret(t, app, repo.Id, "")

	rec := postWebhook(t, mux, stack.Id, []byte(`{"ref":"refs/heads/main"}`), "")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWebhookRejectsMissingSignature(t *testing.T) {
	app, mux := setupTestWebhookApp(t)
	repo := createTestRepoWithBranch(t, app, "main")
	stack := createTestStackWithWebhookSecret(t, app, repo.Id, "s3cret")

	rec := postWebhook(t, mux, stack.Id, []byte(`{"ref":"refs/heads/main"}`), "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWebhookRejectsInvalidSignature(t *testing.T) {
	app, mux := setupTestWebhookApp(t)
	repo := createTestRepoWithBranch(t, app, "main")
	stack := createTestStackWithWebhookSecret(t, app, repo.Id, "s3cret")

	body := []byte(`{"ref":"refs/heads/main"}`)
	rec := postWebhook(t, mux, stack.Id, body, signBody("wrong-secret", body))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Fatalf("response leaked secret: %s", rec.Body.String())
	}
}

func TestWebhookTriggersOnValidSignatureAndBranch(t *testing.T) {
	app, mux := setupTestWebhookApp(t)
	repo := createTestRepoWithBranch(t, app, "main")
	stack := createTestStackWithWebhookSecret(t, app, repo.Id, "s3cret")

	body := []byte(`{"ref":"refs/heads/main"}`)
	rec := postWebhook(t, mux, stack.Id, body, signBody("s3cret", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "triggered") {
		t.Fatalf("expected triggered status, got: %s", rec.Body.String())
	}
}

func TestWebhookSkipsOnBranchMismatch(t *testing.T) {
	app, mux := setupTestWebhookApp(t)
	repo := createTestRepoWithBranch(t, app, "main")
	stack := createTestStackWithWebhookSecret(t, app, repo.Id, "s3cret")

	body := []byte(`{"ref":"refs/heads/feature-x"}`)
	rec := postWebhook(t, mux, stack.Id, body, signBody("s3cret", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "skipped") {
		t.Fatalf("expected skipped status, got: %s", rec.Body.String())
	}
}

func TestWebhookRejectsMalformedPayload(t *testing.T) {
	app, mux := setupTestWebhookApp(t)
	repo := createTestRepoWithBranch(t, app, "main")
	stack := createTestStackWithWebhookSecret(t, app, repo.Id, "s3cret")

	body := []byte(`not json`)
	rec := postWebhook(t, mux, stack.Id, body, signBody("s3cret", body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestForceRedeployBodyDefaultsPauseAfterToTrueWhenOmitted(t *testing.T) {
	cases := []struct {
		name string
		json string
		want bool
	}{
		{"field omitted keeps the true default", `{"recreate_containers":true}`, true},
		{"explicit false is respected", `{"recreate_containers":true,"pause_after_redeploy":false}`, false},
		{"explicit true is respected", `{"pause_after_redeploy":true}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := forceRedeployBody{PauseAfter: true}
			if err := json.NewDecoder(strings.NewReader(c.json)).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.PauseAfter != c.want {
				t.Fatalf("PauseAfter = %v, want %v", body.PauseAfter, c.want)
			}
		})
	}
}

func TestWebhookRejectsUnknownStack(t *testing.T) {
	_, mux := setupTestWebhookApp(t)

	rec := postWebhook(t, mux, "nonexistent-stack-id", []byte(`{"ref":"refs/heads/main"}`), "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// capturingTeardownDispatcher is a minimal sync.WorkerDispatcher stub that
// records the last TeardownCommand it was asked to dispatch, so a test can
// assert what the worker actually received (e.g. that a failed env var
// resolution didn't block teardown, just left EnvFileB64 empty).
type capturingTeardownDispatcher struct {
	connected bool
	lastCmd   protocol.TeardownCommand
}

func (d *capturingTeardownDispatcher) Dispatch(ctx context.Context, workerID string, cmd interface{}) (protocol.CommandResult, error) {
	if tc, ok := cmd.(protocol.TeardownCommand); ok {
		d.lastCmd = tc
	}
	return protocol.CommandResult{}, nil
}

func (d *capturingTeardownDispatcher) IsConnected(workerID string) bool {
	return d.connected
}

func setupStackDeleteTestApp(t *testing.T, dispatcher wiresync.WorkerDispatcher) (core.App, http.Handler, *core.Record) {
	t.Helper()
	app := newSetupTestApp(t)
	admin := createTestUser(t, app, "admin-teardown@example.com", "password123", "admin")

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:  app,
			Auth: admin,
			Event: router.Event{
				Response: w,
				Request:  req,
			},
		}, nil
	})

	rr := routeRegistrar{
		r:         r,
		app:       app,
		scheduler: wiresync.NewScheduler(app, dispatcher),
		workerSvc: dispatcher,
	}
	rr.registerStackDeleteRoute()

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return app, mux, admin
}

// createTeardownTestStack creates a stack assigned to workerID with a
// rendered v1 compose revision on disk, so the delete route's teardown path
// (which needs a non-empty rendered compose file) is exercised.
func createTeardownTestStack(t *testing.T, app core.App, repoID, workerID string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		t.Fatalf("find stacks collection: %v", err)
	}
	stack := core.NewRecord(col)
	stack.Set("name", "teardown-stack")
	stack.Set("repository", repoID)
	stack.Set("worker", workerID)
	stack.Set("current_version", 1)
	if err := app.Save(stack); err != nil {
		t.Fatalf("create test stack: %v", err)
	}

	renderer := wiresync.NewRenderer(app)
	revisionPath := renderer.GetRevisionFilePath(stack.Id, 1)
	if err := os.MkdirAll(filepath.Dir(revisionPath), 0755); err != nil {
		t.Fatalf("create revision dir: %v", err)
	}
	if err := os.WriteFile(revisionPath, []byte("services:\n  web:\n    image: nginx:alpine\n"), 0644); err != nil {
		t.Fatalf("write rendered compose file: %v", err)
	}

	return stack
}

func doDeleteStackRequest(t *testing.T, mux http.Handler, stackID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/custom/stacks/"+stackID, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestStackDeleteProceedsWithEmptyEnvWhenLoadStackFails covers the
// intentional fallback added alongside "Don't block stack deletion on
// unresolvable env vars during teardown": `docker compose down` doesn't need
// working secret values, so a stack whose secret env var can't be resolved
// (e.g. Vault backend disabled/misconfigured) must still tear down — the
// worker just gets an empty env file — and the stack/related records must
// only be deleted after that worker dispatch succeeds.
func TestStackDeleteProceedsWithEmptyEnvWhenLoadStackFails(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	dispatcher := &capturingTeardownDispatcher{connected: true}
	app, mux, _ := setupStackDeleteTestApp(t, dispatcher)

	repo, worker := createOverridesTestRepoAndWorker(t, app)
	stack := createTeardownTestStack(t, app, repo.Id, worker.Id)

	envVarCol, err := app.FindCollectionByNameOrId("stack_env_vars")
	if err != nil {
		t.Fatalf("find stack_env_vars collection: %v", err)
	}
	envVar := core.NewRecord(envVarCol)
	envVar.Set("stack", stack.Id)
	envVar.Set("key", "DB_PASS")
	envVar.Set("value", "secret/data/myapp#DB_PASS")
	envVar.Set("secret", true)
	envVar.Set("secret_provider", "vault")
	if err := app.Save(envVar); err != nil {
		t.Fatalf("create secret env var: %v", err)
	}

	rec := doDeleteStackRequest(t, mux, stack.Id)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if dispatcher.lastCmd.EnvFileB64 != "" {
		t.Fatalf("expected empty EnvFileB64 when env resolution fails, got %q", dispatcher.lastCmd.EnvFileB64)
	}
	if dispatcher.lastCmd.ComposeFileB64 == "" {
		t.Fatal("expected the worker to still receive the rendered compose file")
	}

	if _, err := app.FindRecordById("stacks", stack.Id); err == nil {
		t.Fatal("expected stack record to be deleted after successful teardown dispatch")
	}
}

// TestStackDeleteIncludesEnvFileWhenLoadStackSucceeds covers the sibling
// success path of TestStackDeleteProceedsWithEmptyEnvWhenLoadStackFails: when
// env var resolution succeeds, teardown must build and send a real env file
// rather than leaving EnvFileB64 empty.
func TestStackDeleteIncludesEnvFileWhenLoadStackSucceeds(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	dispatcher := &capturingTeardownDispatcher{connected: true}
	app, mux, _ := setupStackDeleteTestApp(t, dispatcher)

	repo, worker := createOverridesTestRepoAndWorker(t, app)
	stack := createTeardownTestStack(t, app, repo.Id, worker.Id)

	envVarCol, err := app.FindCollectionByNameOrId("stack_env_vars")
	if err != nil {
		t.Fatalf("find stack_env_vars collection: %v", err)
	}
	envVar := core.NewRecord(envVarCol)
	envVar.Set("stack", stack.Id)
	envVar.Set("key", "APP_ENV")
	envVar.Set("value", "production")
	if err := app.Save(envVar); err != nil {
		t.Fatalf("create plain env var: %v", err)
	}

	rec := doDeleteStackRequest(t, mux, stack.Id)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if dispatcher.lastCmd.EnvFileB64 == "" {
		t.Fatal("expected non-empty EnvFileB64 when env resolution succeeds")
	}
	decoded, err := base64.StdEncoding.DecodeString(dispatcher.lastCmd.EnvFileB64)
	if err != nil {
		t.Fatalf("decode EnvFileB64: %v", err)
	}
	if !strings.Contains(string(decoded), "APP_ENV=production") {
		t.Fatalf("expected env file to contain APP_ENV=production, got %q", decoded)
	}
}

func TestComposeServiceScale(t *testing.T) {
	cases := []struct {
		name string
		svc  map[string]interface{}
		want int
	}{
		{
			name: "top-level scale takes precedence over deploy.replicas",
			svc: map[string]interface{}{
				"scale": 3,
				"deploy": map[string]interface{}{
					"replicas": 5,
				},
			},
			want: 3,
		},
		{
			name: "falls back to deploy.replicas when scale absent",
			svc: map[string]interface{}{
				"deploy": map[string]interface{}{
					"replicas": 4,
				},
			},
			want: 4,
		},
		{
			name: "defaults to 1 when neither scale nor deploy.replicas declared",
			svc:  map[string]interface{}{},
			want: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := composeServiceScale(c.svc)
			if got != c.want {
				t.Fatalf("composeServiceScale() = %d, want %d", got, c.want)
			}
		})
	}
}
