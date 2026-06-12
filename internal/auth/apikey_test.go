package auth

import (
	"net/http"
	"testing"
	"time"
)

func TestGenerateAPIKey(t *testing.T) {
	key, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if len(key) == 0 {
		t.Fatal("expected non-empty key")
	}
	if key[:len(apiKeyPrefix)] != apiKeyPrefix {
		t.Fatalf("expected key to start with %q, got %q", apiKeyPrefix, key[:len(apiKeyPrefix)])
	}
}

func TestGenerateAPIKeyUniqueness(t *testing.T) {
	a, _ := GenerateAPIKey()
	b, _ := GenerateAPIKey()
	if a == b {
		t.Fatal("two generated keys must not be equal")
	}
}

func TestHashAPIKeyDeterministic(t *testing.T) {
	key := "wireops_sk_testvalue"
	first := HashAPIKey(key)
	second := HashAPIKey(key)
	if first != second {
		t.Fatalf("HashAPIKey must return the same value for the same input: %q != %q", first, second)
	}
}

func TestHashAPIKeyDifferentInputs(t *testing.T) {
	if HashAPIKey("key1") == HashAPIKey("key2") {
		t.Fatal("different keys must produce different hashes")
	}
}

func TestHashAPIKeyTrimsSpace(t *testing.T) {
	if HashAPIKey("  key  ") != HashAPIKey("key") {
		t.Fatal("HashAPIKey should trim surrounding whitespace")
	}
}

func TestAPIKeyPrefix(t *testing.T) {
	key := "wireops_sk_abcdefghijklmnopqrstuvwxyz"
	prefix := APIKeyPrefix(key)
	if len(prefix) != 16 {
		t.Fatalf("expected prefix length 16, got %d", len(prefix))
	}
	if prefix != key[:16] {
		t.Fatalf("expected %q, got %q", key[:16], prefix)
	}
}

func TestAPIKeyPrefixShortKey(t *testing.T) {
	short := "abc"
	if APIKeyPrefix(short) != short {
		t.Fatalf("short key should be returned as-is, got %q", APIKeyPrefix(short))
	}
}

func TestLastUsedThrottleConstant(t *testing.T) {
	if lastUsedThrottle != 5*time.Minute {
		t.Fatalf("expected 5m throttle, got %v", lastUsedThrottle)
	}
}

func TestAPIKeyFromRequestHeader(t *testing.T) {
	key := "wireops_sk_testvalue"
	req := makeTestRequest(t, key, "")
	if got := apiKeyFromRequest(req); got != key {
		t.Fatalf("expected %q, got %q", key, got)
	}
}

func TestAPIKeyFromRequestBearerWithPrefix(t *testing.T) {
	key := apiKeyPrefix + "somerandombytes"
	req := makeTestRequest(t, "", "Bearer "+key)
	if got := apiKeyFromRequest(req); got != key {
		t.Fatalf("expected %q, got %q", key, got)
	}
}

func TestAPIKeyFromRequestBearerWithoutPrefix(t *testing.T) {
	req := makeTestRequest(t, "", "Bearer regularjwttoken")
	if got := apiKeyFromRequest(req); got != "" {
		t.Fatalf("expected empty for non-wireops bearer, got %q", got)
	}
}

func TestAPIKeyFromRequestNil(t *testing.T) {
	if got := apiKeyFromRequest(nil); got != "" {
		t.Fatalf("expected empty for nil request, got %q", got)
	}
}

func makeTestRequest(t *testing.T, headerKey, authHeader string) *http.Request {
	t.Helper()
	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if headerKey != "" {
		req.Header.Set(APIKeyHeader, headerKey)
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	return req
}
