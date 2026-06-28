package crypto

import (
	"encoding/hex"
	"errors"
	"fmt"
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

// ValidateSecretKey verifies that SECRET_KEY is present and can be normalized
// to the 32-byte AES key used for encrypted secrets.
func ValidateSecretKey(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("SECRET_KEY is required")
	}
	if NormalizeSecretKey(raw) == nil {
		return fmt.Errorf("SECRET_KEY must be a raw 32-byte key or a 64-character hex-encoded 32-byte key (got %d characters)", len(raw))
	}
	return nil
}
