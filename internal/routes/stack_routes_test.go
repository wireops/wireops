package routes

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	_ "github.com/wireops/wireops/pb_migrations"

	"github.com/wireops/wireops/internal/crypto"
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

func TestWebhookRejectsUnknownStack(t *testing.T) {
	_, mux := setupTestWebhookApp(t)

	rec := postWebhook(t, mux, "nonexistent-stack-id", []byte(`{"ref":"refs/heads/main"}`), "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}
