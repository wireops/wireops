package crypto

import (
	"encoding/hex"
	"strings"
)

// NormalizeSecretKey accepts either a raw 32-byte key or the documented
// 64-character hex encoding of a 32-byte key.
func NormalizeSecretKey(raw string) []byte {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if len(raw) == 64 {
		if decoded, err := hex.DecodeString(raw); err == nil && len(decoded) == 32 {
			return decoded
		}
		return nil
	}
	if len(raw) == 32 {
		return []byte(raw)
	}
	return nil
}
