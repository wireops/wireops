package crypto

import (
	"encoding/hex"
	"strings"
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

	if got := NormalizeSecretKey("not-a-supported-secret-key-format"); got != nil {
		t.Fatalf("expected unsupported key length to be rejected")
	}

	if got := NormalizeSecretKey("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"); got != nil {
		t.Fatalf("expected invalid 64-character hex key to be rejected")
	}
}

func TestValidateSecretKey(t *testing.T) {
	raw := "12345678901234567890123456789012"
	hexKey := hex.EncodeToString([]byte(raw))

	tests := []struct {
		name    string
		key     string
		wantErr string
	}{
		{
			name: "raw 32-byte key",
			key:  raw,
		},
		{
			name: "hex-encoded 32-byte key",
			key:  hexKey,
		},
		{
			name:    "missing key",
			key:     "",
			wantErr: "SECRET_KEY is required",
		},
		{
			name:    "unsupported length",
			key:     "short",
			wantErr: "raw 32-byte key or a 64-character hex-encoded 32-byte key",
		},
		{
			name:    "invalid hex",
			key:     strings.Repeat("z", 64),
			wantErr: "raw 32-byte key or a 64-character hex-encoded 32-byte key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSecretKey(tt.key)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateSecretKey() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateSecretKey() expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateSecretKey() error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}
