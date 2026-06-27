package crypto

import (
	"encoding/hex"
	"testing"
)

func TestNormalizeSecretKey(t *testing.T) {
	raw := "12345678901234567890123456789012"
	if got := NormalizeSecretKey(raw); string(got) != raw {
		t.Fatalf("expected raw 32-byte key to remain unchanged")
	}

	hexKey := hex.EncodeToString([]byte(raw))
	if got := NormalizeSecretKey(hexKey); string(got) != raw {
		t.Fatalf("expected hex key to decode to raw bytes")
	}
}
