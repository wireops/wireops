package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/wireops/wireops/internal/integrations"

	// Blank-imported so its descriptor (Encrypted:true on "secret") is
	// registered in this test binary's global integrations registry —
	// dispatch() sources its config via integrations.Store, which derives
	// EncryptedKeys() from the registered Descriptor. In production
	// cmd/serve.go blank-imports every provider before any request is
	// served; see the identical comment in
	// internal/secrets/test_helpers_test.go.
	_ "github.com/wireops/wireops/internal/integrations/webhook"
)

const testDispatchSecretKey = "abcdefghijklmnopqrstuvwxyz012345" // 32 bytes

func newDispatchTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	col := core.NewBaseCollection("integrations")
	col.Fields.Add(&core.TextField{Name: "slug", Required: true})
	col.Fields.Add(&core.BoolField{Name: "enabled"})
	col.Fields.Add(&core.JSONField{Name: "config"})
	if err := app.Save(col); err != nil {
		t.Fatalf("save integrations collection: %v", err)
	}
	return app
}

// TestDispatchSourcesConfigFromStore proves dispatch()'s Store-sourced
// config path decrypts an Encrypted field (webhook's "secret") correctly
// and dispatches using it — the one representative-slug case this commit
// needs; notify_test.go's broader provider-level coverage (Send/BuildConfig
// per-provider) is unaffected since dispatch() only changed where its
// config map comes from, not how it's built or dispatched from there.
// webhook (rather than discord/slack) avoids their https-host-allowlist
// validation, which a plain httptest.Server can't satisfy.
func TestDispatchSourcesConfigFromStore(t *testing.T) {
	t.Setenv("SECRET_KEY", testDispatchSecretKey)

	const wantSecret = "hmac-secret"
	received := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, []byte(wantSecret))
		mac.Write(body)
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if r.Header.Get("X-wireops-Signature") == expected {
			received <- expected
		} else {
			received <- ""
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	app := newDispatchTestApp(t)
	store := integrations.NewStore(app, []byte(testDispatchSecretKey))
	if err := store.Save("webhook", true, map[string]any{
		"url":    srv.URL,
		"secret": wantSecret,
		"events": []any{string(SyncDone)},
	}); err != nil {
		t.Fatalf("save webhook config: %v", err)
	}

	n := New(app)
	n.Dispatch(context.Background(), Payload{
		Event:     SyncDone,
		StackID:   "stack-1",
		StackName: "Stack One",
		Trigger:   "manual",
		CommitSHA: "abc123",
	})
	n.Wait()

	select {
	case sig := <-received:
		if sig == "" {
			t.Fatal("webhook request's HMAC signature did not match the expected secret — dispatch() likely used the still-encrypted value from Store")
		}
	default:
		t.Fatal("expected dispatch() to POST to the webhook URL loaded via Store")
	}
}
