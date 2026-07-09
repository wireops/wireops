package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return sha256Prefix + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignatureValid(t *testing.T) {
	secret := "topsecret"
	body := []byte(`{"ref":"refs/heads/main"}`)
	header := sign(secret, body)

	if !VerifySignature(secret, body, header) {
		t.Fatal("expected valid signature to verify")
	}
}

func TestVerifySignatureInvalid(t *testing.T) {
	secret := "topsecret"
	body := []byte(`{"ref":"refs/heads/main"}`)
	header := sign("wrong-secret", body)

	if VerifySignature(secret, body, header) {
		t.Fatal("expected mismatched signature to fail")
	}
}

func TestVerifySignatureTamperedBody(t *testing.T) {
	secret := "topsecret"
	header := sign(secret, []byte(`{"ref":"refs/heads/main"}`))

	if VerifySignature(secret, []byte(`{"ref":"refs/heads/evil"}`), header) {
		t.Fatal("expected tampered body to fail verification")
	}
}

func TestVerifySignatureMissingHeader(t *testing.T) {
	if VerifySignature("topsecret", []byte("body"), "") {
		t.Fatal("expected empty header to fail")
	}
}

func TestVerifySignatureEmptySecret(t *testing.T) {
	body := []byte("body")
	header := sign("some-secret", body)
	if VerifySignature("", body, header) {
		t.Fatal("expected empty secret to fail")
	}
}

func TestVerifySignatureMissingPrefix(t *testing.T) {
	secret := "topsecret"
	body := []byte("body")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	header := hex.EncodeToString(mac.Sum(nil)) // no "sha256=" prefix

	if VerifySignature(secret, body, header) {
		t.Fatal("expected header without sha256= prefix to fail")
	}
}

func TestVerifySignatureInvalidHex(t *testing.T) {
	if VerifySignature("topsecret", []byte("body"), "sha256=not-hex-zzz") {
		t.Fatal("expected non-hex signature to fail")
	}
}

func TestRefFromPayloadValid(t *testing.T) {
	ref, ok := RefFromPayload([]byte(`{"ref":"refs/heads/develop","other":"field"}`))
	if !ok {
		t.Fatal("expected valid JSON to parse")
	}
	if ref != "refs/heads/develop" {
		t.Fatalf("expected ref refs/heads/develop, got %q", ref)
	}
}

func TestRefFromPayloadMissingRef(t *testing.T) {
	ref, ok := RefFromPayload([]byte(`{"other":"field"}`))
	if !ok {
		t.Fatal("expected valid JSON without ref to still parse")
	}
	if ref != "" {
		t.Fatalf("expected empty ref, got %q", ref)
	}
}

func TestRefFromPayloadMalformed(t *testing.T) {
	if _, ok := RefFromPayload([]byte(`not json`)); ok {
		t.Fatal("expected malformed JSON to fail")
	}
}

func TestBranchFromRefWithPrefix(t *testing.T) {
	if got := BranchFromRef("refs/heads/main"); got != "main" {
		t.Fatalf("expected main, got %q", got)
	}
}

func TestBranchFromRefWithoutPrefix(t *testing.T) {
	if got := BranchFromRef("main"); got != "main" {
		t.Fatalf("expected main, got %q", got)
	}
}
