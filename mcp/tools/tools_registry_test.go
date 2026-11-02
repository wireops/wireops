package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wireops/wireops/internal/auth"
	mcpauth "github.com/wireops/wireops/mcp/auth"
	"github.com/wireops/wireops/mcp/client"
	"github.com/wireops/wireops/mcp/models"
)

// whoamiKey is a dedicated cache key so this test never reads or pollutes an
// entry another test stored under the shared ctxWithKey key.
func whoamiTestCtx(key string) context.Context {
	return mcpauth.WithAPIKey(context.Background(), key)
}

func TestWhoamiReportsCacheFreshness(t *testing.T) {
	var commit atomic.Value
	commit.Store("aaaaaaa")
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		writeFixture(w, fmt.Sprintf(`{"role":"operator","version":"0.2.11","commit":%q,"scan_period_seconds":10}`, commit.Load()))
	}))
	defer srv.Close()

	handler := whoami(client.New(srv.URL))
	key := "wireops_sk_whoami_freshness"
	ctx := whoamiTestCtx(key)

	call := func(t *testing.T, refresh bool) map[string]interface{} {
		t.Helper()
		_, out, err := handler(ctx, nil, models.WhoamiInput{Refresh: refresh})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		result, ok := out.(map[string]interface{})
		if !ok {
			t.Fatalf("expected map output, got %T", out)
		}
		return result
	}

	first := call(t, false)
	if first["cache"] != "fresh" || first["commit"] != "aaaaaaa" {
		t.Fatalf("expected a fresh first answer, got %#v", first)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected 1 server hit, got %d", hits.Load())
	}

	second := call(t, false)
	if second["cache"] != "cache" || second["commit"] != "aaaaaaa" {
		t.Fatalf("expected a cached second answer, got %#v", second)
	}
	if hits.Load() != 1 {
		t.Fatalf("cached call must not hit the server, got %d hits", hits.Load())
	}
	if _, present := second["cache_age_seconds"]; !present {
		t.Fatalf("cached answer must expose its age, got %#v", second)
	}
	if _, present := second["fetched_at"]; !present {
		t.Fatalf("cached answer must expose its observation time, got %#v", second)
	}
	// The shared cached map must not carry the per-call freshness fields.
	cached, ok := whoamiCache.get(key)
	if !ok {
		t.Fatal("expected the entry to be cached")
	}
	if _, polluted := cached["cache"]; polluted {
		t.Fatalf("freshness fields leaked into the shared cache entry: %#v", cached)
	}

	// The server moves to a new revision: only a refresh may observe it.
	commit.Store("bbbbbbb")
	third := call(t, false)
	if third["commit"] != "aaaaaaa" || third["cache"] != "cache" {
		t.Fatalf("an ordinary call must keep serving the cached revision, got %#v", third)
	}
	fourth := call(t, true)
	if fourth["cache"] != "fresh" || fourth["commit"] != "bbbbbbb" {
		t.Fatalf("refresh must observe the new revision, got %#v", fourth)
	}
	if hits.Load() != 2 {
		t.Fatalf("expected 2 server hits after the refresh, got %d", hits.Load())
	}
	if got := fourth["cache_age_seconds"]; got != 0 {
		t.Fatalf("a fresh answer has age 0, got %#v", got)
	}
}

func TestWhoamiRefreshFailureDoesNotFallBackToCache(t *testing.T) {
	healthy := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		writeFixture(w, `{"role":"operator","commit":"aaaaaaa","scan_period_seconds":10}`)
	}))
	defer srv.Close()

	handler := whoami(client.New(srv.URL))
	ctx := whoamiTestCtx("wireops_sk_whoami_refresh_fail")

	if _, _, err := handler(ctx, nil, models.WhoamiInput{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	healthy = false
	_, _, err := handler(ctx, nil, models.WhoamiInput{Refresh: true})
	if err == nil {
		t.Fatal("expected the refresh failure to surface, not to be answered from the cache")
	}
}

func TestTestRegistryCredentialPostsOnlyCredentialID(t *testing.T) {
	rec := newRecorder(map[string]string{
		"POST /api/custom/registry-credentials/test": `{"success":true,"source":"persisted","credential_id":"cred00000000001"}`,
	})
	defer rec.close()

	handler := testRegistryCredential(rec.client())
	_, out, err := handler(ctxWithKey(), nil, models.TestRegistryCredentialInput{Credential: "cred00000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == nil {
		t.Fatal("expected non-nil output")
	}
	last := rec.last(t)
	if last.Method != http.MethodPost || last.Path != "/api/custom/registry-credentials/test" {
		t.Fatalf("unexpected request %s %s", last.Method, last.Path)
	}
	if len(last.Body) != 1 || last.Body["credential_id"] != "cred00000000001" {
		t.Fatalf("persisted probe must send only credential_id, got %#v", last.Body)
	}
}

func TestTestRegistryCredentialResolvesName(t *testing.T) {
	rec := newRecorder(map[string]string{
		"GET /api/collections/registry_credentials/records": `{"items":[{"id":"cred00000000001","name":"ghcr prod"}]}`,
		"POST /api/custom/registry-credentials/test":        `{"success":false,"source":"persisted","error":"authentication rejected (HTTP 401)","diagnostic":{"stage":"challenge","category":"auth_rejected","http_status":401,"message":"authentication rejected (HTTP 401)","hint":"..."}}`,
	})
	defer rec.close()

	handler := testRegistryCredential(rec.client())
	_, _, err := handler(ctxWithKey(), nil, models.TestRegistryCredentialInput{Credential: "ghcr prod"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	reqs := rec.all()
	if len(reqs) != 2 {
		t.Fatalf("expected a name lookup then the probe, got %d requests", len(reqs))
	}
	if !strings.Contains(reqs[0].Query, "filter=name%3D%27ghcr") {
		t.Fatalf("expected the name lookup filter, got %q", reqs[0].Query)
	}
	if reqs[1].Body["credential_id"] != "cred00000000001" {
		t.Fatalf("expected the resolved id in the probe, got %#v", reqs[1].Body)
	}
}

func TestTestRegistryCredentialRequiresCredential(t *testing.T) {
	handler := testRegistryCredential(client.New("http://127.0.0.1:1"))
	if _, _, err := handler(ctxWithKey(), nil, models.TestRegistryCredentialInput{}); err == nil {
		t.Fatal("expected an error for an empty credential")
	}
}

func TestSetStackSecretInlineValueStaysOutOfTheResult(t *testing.T) {
	rec := newRecorder(map[string]string{
		"POST /api/custom/stacks/stack0000000001/env-vars/bulk": `{"upserted":1}`,
	})
	defer rec.close()

	handler := setStackSecret(rec.client())
	_, out, err := handler(ctxWithKey(), nil, models.SetStackSecretInput{
		StackID: "stack0000000001",
		Key:     "DB_PASSWORD",
		Value:   "s3cr3t-value",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last := rec.last(t)
	if last.Method != http.MethodPost || last.Path != "/api/custom/stacks/stack0000000001/env-vars/bulk" {
		t.Fatalf("unexpected request %s %s", last.Method, last.Path)
	}
	if last.Body["mode"] != "append" {
		t.Fatalf("expected append mode, got %#v", last.Body)
	}
	vars, ok := last.Body["vars"].([]interface{})
	if !ok || len(vars) != 1 {
		t.Fatalf("expected one var entry, got %#v", last.Body["vars"])
	}
	entry, ok := vars[0].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected var entry %#v", vars[0])
	}
	if entry["key"] != "DB_PASSWORD" || entry["value"] != "s3cr3t-value" || entry["secret"] != true {
		t.Fatalf("unexpected var entry %#v", entry)
	}
	result, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(result), "s3cr3t") {
		t.Fatalf("the secret value leaked into the tool result: %s", result)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["secret"] != true || decoded["secret_provider"] != "internal" || decoded["key"] != "DB_PASSWORD" {
		t.Fatalf("expected metadata-only result, got %s", result)
	}
	if decoded["elicited"] != false {
		t.Fatalf("an inline value is not elicited, got %s", result)
	}
}

func TestSetStackSecretWithoutSessionOrValueAsksForExplicitValue(t *testing.T) {
	handler := setStackSecret(client.New("http://127.0.0.1:1"))
	// A nil *mcp.CallToolRequest models a call path without a session.
	_, _, err := handler(ctxWithKey(), nil, models.SetStackSecretInput{StackID: "stack0000000001", Key: "DB_PASSWORD"})
	if err == nil || !strings.Contains(err.Error(), "pass value explicitly") {
		t.Fatalf("expected guidance to pass the value explicitly, got %v", err)
	}
}

func TestSetStackSecretRequiresKey(t *testing.T) {
	handler := setStackSecret(client.New("http://127.0.0.1:1"))
	if _, _, err := handler(ctxWithKey(), nil, models.SetStackSecretInput{StackID: "stack0000000001", Value: "x"}); err == nil {
		t.Fatal("expected an error for an empty key")
	}
}

type fakePrompter struct {
	result *mcp.ElicitResult
	err    error
	called bool
}

func (f *fakePrompter) Elicit(_ context.Context, params *mcp.ElicitParams) (*mcp.ElicitResult, error) {
	f.called = true
	return f.result, f.err
}

func TestResolveSecretValue(t *testing.T) {
	t.Run("provided-wins-without-prompting", func(t *testing.T) {
		p := &fakePrompter{}
		value, elicited, err := resolveSecretValue(context.Background(), p, "inline", "KEY", "stack1")
		if err != nil || value != "inline" || elicited {
			t.Fatalf("got %q %v %v", value, elicited, err)
		}
		if p.called {
			t.Fatal("an explicit value must not prompt")
		}
	})

	t.Run("no-prompter", func(t *testing.T) {
		_, _, err := resolveSecretValue(context.Background(), nil, "", "KEY", "stack1")
		if err == nil || !strings.Contains(err.Error(), "pass value explicitly") {
			t.Fatalf("expected explicit-value guidance, got %v", err)
		}
	})

	t.Run("prompt-accept", func(t *testing.T) {
		p := &fakePrompter{result: &mcp.ElicitResult{Action: "accept", Content: map[string]interface{}{"value": "prompted-value"}}}
		value, elicited, err := resolveSecretValue(context.Background(), p, "", "KEY", "stack1")
		if err != nil || value != "prompted-value" || !elicited {
			t.Fatalf("got %q %v %v", value, elicited, err)
		}
		if !p.called {
			t.Fatal("expected the prompter to be used")
		}
	})

	t.Run("prompt-decline", func(t *testing.T) {
		p := &fakePrompter{result: &mcp.ElicitResult{Action: "decline"}}
		_, _, err := resolveSecretValue(context.Background(), p, "", "KEY", "stack1")
		if err == nil || !strings.Contains(err.Error(), "nothing was written") {
			t.Fatalf("expected a refusal with nothing written, got %v", err)
		}
	})

	t.Run("prompt-error-mentions-explicit-path", func(t *testing.T) {
		p := &fakePrompter{err: errors.New("client does not support elicitation")}
		_, _, err := resolveSecretValue(context.Background(), p, "", "KEY", "stack1")
		if err == nil || !strings.Contains(err.Error(), "pass value explicitly") {
			t.Fatalf("expected the explicit fallback to be named, got %v", err)
		}
	})

	t.Run("prompt-accept-empty", func(t *testing.T) {
		p := &fakePrompter{result: &mcp.ElicitResult{Action: "accept", Content: map[string]interface{}{"value": ""}}}
		_, _, err := resolveSecretValue(context.Background(), p, "", "KEY", "stack1")
		if err == nil || !strings.Contains(err.Error(), "empty") {
			t.Fatalf("expected an empty-value refusal, got %v", err)
		}
	})
}

func TestRegistryToolsForwardAPIKey(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get(auth.APIKeyHeader)
		writeFixture(w, `{"success":true,"source":"persisted"}`)
	}))
	defer srv.Close()

	handler := testRegistryCredential(client.New(srv.URL))
	if _, _, err := handler(ctxWithKey(), nil, models.TestRegistryCredentialInput{Credential: "cred00000000001"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotHeader != "wireops_sk_test" {
		t.Fatalf("expected the caller's API key to be forwarded, got %q", gotHeader)
	}
}
