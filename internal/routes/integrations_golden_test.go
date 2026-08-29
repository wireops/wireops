package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	_ "github.com/wireops/wireops/internal/gitprovider/github"
	_ "github.com/wireops/wireops/internal/gitprovider/gitlab"
	_ "github.com/wireops/wireops/internal/integrations/caddy"
	_ "github.com/wireops/wireops/internal/integrations/discord"
	_ "github.com/wireops/wireops/internal/integrations/dozzle"
	_ "github.com/wireops/wireops/internal/integrations/github"
	_ "github.com/wireops/wireops/internal/integrations/gitlab"
	_ "github.com/wireops/wireops/internal/integrations/infisical"
	_ "github.com/wireops/wireops/internal/integrations/nginxproxymanager"
	_ "github.com/wireops/wireops/internal/integrations/ntfy"
	_ "github.com/wireops/wireops/internal/integrations/s3"
	_ "github.com/wireops/wireops/internal/integrations/slack"
	_ "github.com/wireops/wireops/internal/integrations/sops"
	_ "github.com/wireops/wireops/internal/integrations/traefik"
	_ "github.com/wireops/wireops/internal/integrations/vault"
	_ "github.com/wireops/wireops/internal/integrations/webhook"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/rbac"
)

// This file locks the HTTP-level contract of the current (pre-refactor)
// internal/integrations + internal/routes integration endpoints, ahead of
// the descriptor-driven registry refactor. Every assertion here documents
// *current* behavior verbatim — it is not a spec for how things should be,
// only a tripwire for what changes.

// goldenIntegrationOutput mirrors routes_register.go's inline
// IntegrationOutput struct so the golden test decodes the exact same JSON
// shape the handler emits.
type goldenIntegrationOutput struct {
	Slug     string                 `json:"slug"`
	Name     string                 `json:"name"`
	Category string                 `json:"category"`
	Enabled  bool                   `json:"enabled"`
	Locked   bool                   `json:"locked"`
	Config   map[string]interface{} `json:"config"`
}

// goldenExpectedIntegration is the exact current Name/Category string
// literal pulled from each provider's source file under
// internal/integrations/<slug>/ — see integrations_golden_test.go's setup
// comment for where each one was read from.
type goldenExpectedIntegration struct {
	Name     string
	Category string
}

var goldenExpectedIntegrations = map[string]goldenExpectedIntegration{
	"caddy":               {"Caddy", "Reverse Proxy"},
	"dozzle":              {"Dozzle", "Logging"},
	"nginx-proxy-manager": {"Nginx Proxy Manager", "Reverse Proxy"},
	"traefik":             {"Traefik", "Reverse Proxy"},
	"discord":             {"Discord", "Notification"},
	"slack":               {"Slack", "Notification"},
	"ntfy":                {"Ntfy", "Notification"},
	"webhook":             {"Webhook", "Notification"},
	"vault":               {"HashiCorp Vault", "Secret Backend"},
	"infisical":           {"Infisical", "Secret Backend"},
	"s3":                  {"S3 Storage", "Storage Backend"},
	"sops":                {"SOPS", "Secret Backend"},
	"github":              {"GitHub", "Source Control"},
	"gitlab":              {"GitLab", "Source Control"},
}

// goldenStatusDispatcher is a sync.WorkerDispatcher stub whose Dispatch
// response is configurable per-test, so the GET
// /api/custom/stacks/{id}/integration-actions handler (which round-trips a
// protocol.GetStatusCommand through the worker) can be exercised without a
// real worker WebSocket connection.
type goldenStatusDispatcher struct {
	connected bool
	result    protocol.CommandResult
	err       error
}

func (d *goldenStatusDispatcher) Dispatch(ctx context.Context, workerID string, cmd interface{}) (protocol.CommandResult, error) {
	return d.result, d.err
}

func (d *goldenStatusDispatcher) IsConnected(workerID string) bool {
	return d.connected
}

// setupIntegrationsGoldenTestApp wires only the integration routes
// (registerIntegrationRoutes covers GET/PUT/DELETE /api/custom/integrations
// and both /test and /integration-actions) against an authenticated admin
// request, optionally with a worker dispatcher for the integration-actions
// test.
func setupIntegrationsGoldenTestApp(t *testing.T, dispatcher *goldenStatusDispatcher) (core.App, http.Handler, *core.Record) {
	t.Helper()
	t.Setenv("SECRET_KEY", testSecretBackendKey)
	// github/gitlab's "enabled" is derived live from provider.Configured(),
	// which reads these env vars (see registerIntegrationRoutes) — clear them
	// so an ambient value in the test process can't make the "enabled=false"
	// assertions below flaky.
	t.Setenv("GITHUB_OAUTH_CLIENT_ID", "")
	t.Setenv("GITHUB_OAUTH_CLIENT_SECRET", "")
	t.Setenv("GITLAB_OAUTH_CLIENT_ID", "")
	t.Setenv("GITLAB_OAUTH_CLIENT_SECRET", "")

	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	admin := createTestUser(t, app, "admin@example.com", "Password1!", rbac.RoleAdmin)

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:   app,
			Event: router.Event{Response: w, Request: req},
			Auth:  admin,
		}, nil
	})

	rr := routeRegistrar{r: r, app: app}
	if dispatcher != nil {
		rr.workerSvc = dispatcher
	}
	rr.registerIntegrationRoutes(crypto.NormalizeSecretKey(testSecretBackendKey))

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return app, mux, admin
}

func doGoldenJSONRequest(t *testing.T, mux http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeGoldenIntegrationList(t *testing.T, rec *httptest.ResponseRecorder) map[string]goldenIntegrationOutput {
	t.Helper()
	var list []goldenIntegrationOutput
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode integration list: %v (body: %s)", err, rec.Body.String())
	}
	bySlug := make(map[string]goldenIntegrationOutput, len(list))
	for _, item := range list {
		bySlug[item.Slug] = item
	}
	return bySlug
}

// TestIntegrationsListReturnsAllSlugsWithMetadata pins the current set of 14
// registered integration slugs and their exact name/category string
// literals, and the current locked/enabled defaults:
//   - "sops" is seeded locked+enabled=true by migration 53.
//   - every other slug defaults enabled=false with no DB row.
//   - "sops", "github" and "gitlab" are the only slugs in alwaysLockedIntegrationSlugs.
//
// Assertions are keyed by slug (a map), never by array index/order, since
// integrations.All() today iterates a Go map and has no stable order.
func TestIntegrationsListReturnsAllSlugsWithMetadata(t *testing.T) {
	_, mux, _ := setupIntegrationsGoldenTestApp(t, nil)

	rec := doGoldenJSONRequest(t, mux, http.MethodGet, "/api/custom/integrations", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	bySlug := decodeGoldenIntegrationList(t, rec)
	if len(bySlug) != 14 {
		t.Fatalf("expected 14 integrations, got %d: %+v", len(bySlug), bySlug)
	}

	for slug, expected := range goldenExpectedIntegrations {
		item, ok := bySlug[slug]
		if !ok {
			t.Fatalf("expected slug %q in response, got %+v", slug, bySlug)
		}
		if item.Name != expected.Name {
			t.Errorf("slug %q: expected name %q, got %q", slug, expected.Name, item.Name)
		}
		if item.Category != expected.Category {
			t.Errorf("slug %q: expected category %q, got %q", slug, expected.Category, item.Category)
		}

		switch slug {
		case "sops":
			if !item.Enabled {
				t.Errorf("slug %q: expected enabled=true (seeded by migration 53), got false", slug)
			}
		default:
			if item.Enabled {
				t.Errorf("slug %q: expected enabled=false with no DB row, got true", slug)
			}
		}

		if slug == "sops" || slug == "github" || slug == "gitlab" {
			if !item.Locked {
				t.Errorf("slug %q: expected locked=true, got false", slug)
			}
		} else {
			if item.Locked {
				t.Errorf("slug %q: expected locked=false, got true", slug)
			}
		}
	}
}

// TestIntegrationsConfigMaskingRoundTrip saves a vault config containing a
// sensitive "token" field, then re-fetches the integration list and asserts
// the token comes back masked ("••••••••"), never the raw value.
func TestIntegrationsConfigMaskingRoundTrip(t *testing.T) {
	_, mux, _ := setupIntegrationsGoldenTestApp(t, nil)

	putRec := doGoldenJSONRequest(t, mux, http.MethodPut, "/api/custom/integrations/vault", map[string]interface{}{
		"enabled": true,
		"config": map[string]interface{}{
			"address": "https://vault.example.com",
			"token":   "s.supersecrettoken",
		},
	})
	if putRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on save, got %d: %s", putRec.Code, putRec.Body.String())
	}

	getRec := doGoldenJSONRequest(t, mux, http.MethodGet, "/api/custom/integrations", nil)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on list, got %d: %s", getRec.Code, getRec.Body.String())
	}
	bySlug := decodeGoldenIntegrationList(t, getRec)

	vault, ok := bySlug["vault"]
	if !ok {
		t.Fatalf("expected vault in response, got %+v", bySlug)
	}
	token, _ := vault.Config["token"].(string)
	if token != "••••••••" {
		t.Fatalf("expected masked token, got %q", token)
	}
	if token == "s.supersecrettoken" {
		t.Fatal("raw secret leaked in list response")
	}
}

// TestIntegrationsPutHappyPathVaultDiscordS3 covers a straightforward save
// for one integration from each of the three PUT-relevant shapes exercised
// elsewhere in this file: a secret-backend (vault), a notification provider
// (discord), and a storage backend (s3).
func TestIntegrationsPutHappyPathVaultDiscordS3(t *testing.T) {
	_, mux, _ := setupIntegrationsGoldenTestApp(t, nil)

	cases := []struct {
		slug   string
		config map[string]interface{}
	}{
		{
			slug: "vault",
			config: map[string]interface{}{
				"address": "https://vault.example.com",
				"token":   "s.mytoken",
			},
		},
		{
			slug: "discord",
			config: map[string]interface{}{
				"url": "https://discord.com/api/webhooks/12345/abcdef",
			},
		},
		{
			slug: "s3",
			config: map[string]interface{}{
				"bucket":     "my-bucket",
				"region":     "us-east-1",
				"access_key": "AKIAEXAMPLE",
				"secret":     "s3cr3t-access-key",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.slug, func(t *testing.T) {
			rec := doGoldenJSONRequest(t, mux, http.MethodPut, "/api/custom/integrations/"+tc.slug, map[string]interface{}{
				"enabled": true,
				"config":  tc.config,
			})
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
			}
			var out map[string]interface{}
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if out["slug"] != tc.slug {
				t.Errorf("expected slug %q, got %v", tc.slug, out["slug"])
			}
			if out["enabled"] != true {
				t.Errorf("expected enabled=true, got %v", out["enabled"])
			}
		})
	}
}

// TestIntegrationsPutMaskedResubmitDoesNotDoubleEncrypt is the exact
// regression this whole refactor is guarding against: saving a real secret,
// re-fetching it (masked), then resubmitting the masked placeholder
// unchanged must NOT re-encrypt (or otherwise mutate) the stored ciphertext.
// It queries the DB record directly to compare — not the masked API
// response, which would hide a double-encryption bug entirely.
func TestIntegrationsPutMaskedResubmitDoesNotDoubleEncrypt(t *testing.T) {
	app, mux, _ := setupIntegrationsGoldenTestApp(t, nil)

	firstRec := doGoldenJSONRequest(t, mux, http.MethodPut, "/api/custom/integrations/vault", map[string]interface{}{
		"enabled": true,
		"config": map[string]interface{}{
			"address": "https://vault.example.com",
			"token":   "s.realtoken",
		},
	})
	if firstRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on first save, got %d: %s", firstRec.Code, firstRec.Body.String())
	}

	storedAfterFirstSave := func() string {
		recs, err := app.FindAllRecords("integrations", dbx.HashExp{"slug": "vault"})
		if err != nil || len(recs) == 0 {
			t.Fatalf("expected a stored vault record, err=%v len=%d", err, len(recs))
		}
		var cfg map[string]interface{}
		if err := recs[0].UnmarshalJSONField("config", &cfg); err != nil {
			t.Fatalf("unmarshal stored config: %v", err)
		}
		token, _ := cfg["token"].(string)
		if token == "" {
			t.Fatal("expected a non-empty stored token ciphertext after first save")
		}
		return token
	}()

	getRec := doGoldenJSONRequest(t, mux, http.MethodGet, "/api/custom/integrations", nil)
	bySlug := decodeGoldenIntegrationList(t, getRec)
	vault, ok := bySlug["vault"]
	if !ok {
		t.Fatalf("expected vault in response, got %+v", bySlug)
	}
	maskedToken, _ := vault.Config["token"].(string)
	if maskedToken != "••••••••" {
		t.Fatalf("expected masked token from GET, got %q", maskedToken)
	}

	resubmitRec := doGoldenJSONRequest(t, mux, http.MethodPut, "/api/custom/integrations/vault", map[string]interface{}{
		"enabled": true,
		"config": map[string]interface{}{
			"address": "https://vault.example.com",
			"token":   maskedToken,
		},
	})
	if resubmitRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on masked resubmit, got %d: %s", resubmitRec.Code, resubmitRec.Body.String())
	}

	recs, err := app.FindAllRecords("integrations", dbx.HashExp{"slug": "vault"})
	if err != nil || len(recs) == 0 {
		t.Fatalf("expected a stored vault record after resubmit, err=%v len=%d", err, len(recs))
	}
	var cfgAfterResubmit map[string]interface{}
	if err := recs[0].UnmarshalJSONField("config", &cfgAfterResubmit); err != nil {
		t.Fatalf("unmarshal stored config after resubmit: %v", err)
	}
	storedAfterResubmit, _ := cfgAfterResubmit["token"].(string)

	if storedAfterResubmit != storedAfterFirstSave {
		t.Fatalf("masked resubmit mutated stored ciphertext:\n  first save: %q\n  after resubmit: %q", storedAfterFirstSave, storedAfterResubmit)
	}
}

// TestIntegrationsPutRequiredFieldValidationError covers the enabled=true
// required-field check: saving vault with enabled=true but no "address"
// must be rejected with 400, since validateRequiredIntegrationConfig only
// runs when body.Enabled is true.
func TestIntegrationsPutRequiredFieldValidationError(t *testing.T) {
	_, mux, _ := setupIntegrationsGoldenTestApp(t, nil)

	rec := doGoldenJSONRequest(t, mux, http.MethodPut, "/api/custom/integrations/vault", map[string]interface{}{
		"enabled": true,
		"config": map[string]interface{}{
			"token": "s.mytoken",
			// "address" deliberately omitted.
		},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out["error"] != "address is required" {
		t.Fatalf("expected exact error %q, got %q", "address is required", out["error"])
	}
}

// TestIntegrationsPutRejectsLockedSlugs covers PUT rejection for the
// always-locked slugs (sops, github, gitlab) — all must 403 with the exact
// current error message, regardless of the request body.
func TestIntegrationsPutRejectsLockedSlugs(t *testing.T) {
	_, mux, _ := setupIntegrationsGoldenTestApp(t, nil)

	for _, slug := range []string{"sops", "github", "gitlab"} {
		t.Run(slug, func(t *testing.T) {
			rec := doGoldenJSONRequest(t, mux, http.MethodPut, "/api/custom/integrations/"+slug, map[string]interface{}{
				"enabled": true,
				"config":  map[string]interface{}{},
			})
			if rec.Code != http.StatusForbidden {
				t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
			}
			var out map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			want := "this integration is always active and cannot be reconfigured"
			if out["error"] != want {
				t.Fatalf("expected exact error %q, got %q", want, out["error"])
			}
		})
	}
}

// TestIntegrationsDeleteRejectsLockedSlugs mirrors the PUT locked-slug
// rejection for DELETE.
func TestIntegrationsDeleteRejectsLockedSlugs(t *testing.T) {
	_, mux, _ := setupIntegrationsGoldenTestApp(t, nil)

	for _, slug := range []string{"sops", "github", "gitlab"} {
		t.Run(slug, func(t *testing.T) {
			rec := doGoldenJSONRequest(t, mux, http.MethodDelete, "/api/custom/integrations/"+slug, nil)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
			}
			var out map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			want := "this integration is always active and cannot be disabled"
			if out["error"] != want {
				t.Fatalf("expected exact error %q, got %q", want, out["error"])
			}
		})
	}
}

// TestIntegrationsDeleteNormalSucceeds covers the DELETE happy path (an
// enabled, unlocked integration): the row is actually removed.
//
// routes_register.go's DELETE handler used to call
// `rr.app.FindRecordsByFilter("integrations", "slug = {:slug}", "", 0, 1, ...)`
// — PocketBase's signature is (filter, sort, limit, offset, params), so that
// passed limit=0 (unlimited) and offset=1, which skipped the one matching
// row entirely, taking the handler's "no record found" branch and returning
// 200 {"status":"deleted"} without ever calling app.Delete. Fixed to
// limit=1, offset=0 so the matching row is actually found and removed.
func TestIntegrationsDeleteNormalSucceeds(t *testing.T) {
	app, mux, _ := setupIntegrationsGoldenTestApp(t, nil)

	saveRec := doGoldenJSONRequest(t, mux, http.MethodPut, "/api/custom/integrations/vault", map[string]interface{}{
		"enabled": true,
		"config": map[string]interface{}{
			"address": "https://vault.example.com",
			"token":   "s.mytoken",
		},
	})
	if saveRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on save, got %d: %s", saveRec.Code, saveRec.Body.String())
	}

	delRec := doGoldenJSONRequest(t, mux, http.MethodDelete, "/api/custom/integrations/vault", nil)
	if delRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on delete, got %d: %s", delRec.Code, delRec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(delRec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out["status"] != "deleted" {
		t.Fatalf("expected status=deleted, got %+v", out)
	}

	recs, err := app.FindAllRecords("integrations", dbx.HashExp{"slug": "vault"})
	if err != nil {
		t.Fatalf("query integrations: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected the vault record to be removed after DELETE, found %d", len(recs))
	}
}

// TestIntegrationsTestDispatchesWebhookEndToEnd spins up an httptest.Server
// acting as the webhook receiver, so a webhook-slug /test call actually
// dispatches a real HTTP request end-to-end, mirroring how
// vault_browse_routes_test.go fakes a Vault server for its own /test route.
func TestIntegrationsTestDispatchesWebhookEndToEnd(t *testing.T) {
	received := make(chan *http.Request, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, mux, _ := setupIntegrationsGoldenTestApp(t, nil)

	rec := doGoldenJSONRequest(t, mux, http.MethodPost, "/api/custom/integrations/webhook/test", map[string]interface{}{
		"enabled": true,
		"config": map[string]interface{}{
			"url": srv.URL,
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out["status"] != "dispatched" {
		t.Fatalf("expected status=dispatched, got %+v", out)
	}

	select {
	case req := <-received:
		if req.Method != http.MethodPost {
			t.Fatalf("expected POST to webhook receiver, got %s", req.Method)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the webhook receiver to be called")
	}
}

// TestIntegrationsTestRejectsNonNotificationSlugs pins the current exact
// error text returned when /test is called for a slug that isn't a
// notification integration (and isn't a registered git provider either).
func TestIntegrationsTestRejectsNonNotificationSlugs(t *testing.T) {
	_, mux, _ := setupIntegrationsGoldenTestApp(t, nil)

	for _, slug := range []string{"traefik", "s3"} {
		t.Run(slug, func(t *testing.T) {
			rec := doGoldenJSONRequest(t, mux, http.MethodPost, "/api/custom/integrations/"+slug+"/test", map[string]interface{}{
				"enabled": true,
				"config":  map[string]interface{}{},
			})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			var out map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			want := "only notification integrations can be tested"
			if out["error"] != want {
				t.Fatalf("expected exact error %q, got %q", want, out["error"])
			}
		})
	}
}

// TestStackIntegrationActionsResolvesCaddyLabelToAction builds one fixture
// stack + an enabled "caddy" integration + a mocked worker status response
// carrying a caddy-recognizable label, and asserts the exact JSON key names
// the handler emits (integration_slug, kind, label, url, icon) — pinning the
// integrations.ContainerAction wire shape so a later Go-type rename
// (ContainerAction -> Action) can be verified byte-for-byte against this
// test.
func TestStackIntegrationActionsResolvesCaddyLabelToAction(t *testing.T) {
	statuses := []map[string]interface{}{
		{
			"ServiceName":   "web",
			"ContainerID":   "container-1",
			"ContainerName": "web-1",
			"Status":        "running",
			"Labels": map[string]string{
				"caddy": "app.example.com",
			},
		},
	}
	statusJSON, err := json.Marshal(statuses)
	if err != nil {
		t.Fatalf("marshal fixture statuses: %v", err)
	}

	dispatcher := &goldenStatusDispatcher{
		connected: true,
		result:    protocol.CommandResult{Output: string(statusJSON)},
	}
	app, mux, _ := setupIntegrationsGoldenTestApp(t, dispatcher)

	repo, worker := createOverridesTestRepoAndWorker(t, app)
	workDir := t.TempDir()
	writeOverridesComposeFile(t, workDir, "name: golden_stack\nservices:\n  web:\n    image: nginx:alpine\n")
	stack := createOverridesTestStack(t, app, repo.Id, worker.Id, workDir)

	integrationsCol, err := app.FindCollectionByNameOrId("integrations")
	if err != nil {
		t.Fatalf("find integrations collection: %v", err)
	}
	caddyRec := core.NewRecord(integrationsCol)
	caddyRec.Set("slug", "caddy")
	caddyRec.Set("enabled", true)
	caddyRec.Set("config", map[string]interface{}{})
	if err := app.Save(caddyRec); err != nil {
		t.Fatalf("save caddy integration config: %v", err)
	}

	rec := doGoldenJSONRequest(t, mux, http.MethodGet, "/api/custom/stacks/"+stack.Id+"/integration-actions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var out map[string][]map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	actions, ok := out["container-1"]
	if !ok || len(actions) != 1 {
		t.Fatalf("expected exactly one action for container-1, got %+v", out)
	}
	action := actions[0]

	for _, key := range []string{"integration_slug", "kind", "label", "url", "icon"} {
		if _, present := action[key]; !present {
			t.Errorf("expected key %q in action JSON, got %+v", key, action)
		}
	}
	if action["integration_slug"] != "caddy" {
		t.Errorf("expected integration_slug=caddy, got %v", action["integration_slug"])
	}
	if action["kind"] != "reverse-proxy" {
		t.Errorf("expected kind=reverse-proxy, got %v", action["kind"])
	}
	if action["label"] != "Open" {
		t.Errorf("expected label=Open, got %v", action["label"])
	}
	if action["url"] != "https://app.example.com" {
		t.Errorf("expected url=https://app.example.com, got %v", action["url"])
	}
	if action["icon"] != "i-lucide-external-link" {
		t.Errorf("expected icon=i-lucide-external-link, got %v", action["icon"])
	}
}
