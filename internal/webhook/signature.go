package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

const GitHubSignatureHeader = "X-Hub-Signature-256"

const sha256Prefix = "sha256="

// VerifySignature validates a "sha256=<hex>" header value against an
// HMAC-SHA256 of body computed with secret, using a constant-time comparison.
func VerifySignature(secret string, body []byte, headerValue string) bool {
	if secret == "" || headerValue == "" {
		return false
	}
	if !strings.HasPrefix(headerValue, sha256Prefix) {
		return false
	}

	sig, err := hex.DecodeString(strings.TrimPrefix(headerValue, sha256Prefix))
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)

	return hmac.Equal(sig, expected)
}

// RefFromPayload extracts the "ref" field from a JSON push payload (e.g. GitHub).
// ok is false if body is not valid JSON.
func RefFromPayload(body []byte) (ref string, ok bool) {
	var payload struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", false
	}
	return payload.Ref, true
}

// BranchFromRef normalizes "refs/heads/main" -> "main".
func BranchFromRef(ref string) string {
	return strings.TrimPrefix(ref, "refs/heads/")
}
