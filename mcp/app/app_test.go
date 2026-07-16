package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wireops/wireops/internal/auth"
	mcpauth "github.com/wireops/wireops/mcp/auth"
)

func TestGetListenAddrDefault(t *testing.T) {
	t.Setenv("MCP_LISTEN_ADDR", "")
	if got := getListenAddr(); got != ":8091" {
		t.Fatalf("expected default :8091, got %q", got)
	}
}

func TestGetListenAddrOverride(t *testing.T) {
	t.Setenv("MCP_LISTEN_ADDR", ":9999")
	if got := getListenAddr(); got != ":9999" {
		t.Fatalf("expected :9999, got %q", got)
	}
}

func TestWithAPIKeyMiddlewareExtractsCustomHeader(t *testing.T) {
	var gotKey string
	var sawKey bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, sawKey = mcpauth.APIKeyFromContext(r.Context())
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set(auth.APIKeyHeader, "wireops_sk_test")
	rec := httptest.NewRecorder()

	withAPIKeyMiddleware(next).ServeHTTP(rec, req)

	if !sawKey || gotKey != "wireops_sk_test" {
		t.Fatalf("expected API key propagated to context, got %q (present=%v)", gotKey, sawKey)
	}
}

func TestWithAPIKeyMiddlewareFallsBackToBearer(t *testing.T) {
	var gotKey string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, _ = mcpauth.APIKeyFromContext(r.Context())
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer wireops_sk_bearer")
	rec := httptest.NewRecorder()

	withAPIKeyMiddleware(next).ServeHTTP(rec, req)

	if gotKey != "wireops_sk_bearer" {
		t.Fatalf("expected bearer token used as API key, got %q", gotKey)
	}
}

func TestWithAPIKeyMiddlewareNoKeyPresent(t *testing.T) {
	var sawKey bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawKey = mcpauth.APIKeyFromContext(r.Context())
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	rec := httptest.NewRecorder()

	withAPIKeyMiddleware(next).ServeHTTP(rec, req)

	if sawKey {
		t.Fatal("expected no API key on context when none supplied")
	}
}
