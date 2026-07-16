package auth

import (
	"context"
	"testing"
)

func TestWithAPIKeyRoundTrip(t *testing.T) {
	ctx := WithAPIKey(context.Background(), "wireops_sk_test")

	got, ok := APIKeyFromContext(ctx)
	if !ok {
		t.Fatal("expected API key to be present")
	}
	if got != "wireops_sk_test" {
		t.Fatalf("expected wireops_sk_test, got %q", got)
	}
}

func TestAPIKeyFromContextMissing(t *testing.T) {
	_, ok := APIKeyFromContext(context.Background())
	if ok {
		t.Fatal("expected no API key on empty context")
	}
}

func TestWithAPIKeyEmptyString(t *testing.T) {
	ctx := WithAPIKey(context.Background(), "")

	_, ok := APIKeyFromContext(ctx)
	if ok {
		t.Fatal("expected empty API key to not count as present")
	}
}
