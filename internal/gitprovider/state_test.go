package gitprovider

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wireops/wireops/internal/crypto"
)

const testSecretKey = "0123456789abcdef0123456789abcdef"

func TestSignStateVerifyStateRoundTrip(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretKey)

	state, err := SignState("user-123")
	if err != nil {
		t.Fatalf("sign state: %v", err)
	}

	userID, err := VerifyState(state)
	if err != nil {
		t.Fatalf("verify state: %v", err)
	}
	if userID != "user-123" {
		t.Fatalf("userID = %q, want %q", userID, "user-123")
	}
}

func TestVerifyState(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretKey)

	valid, err := SignState("user-123")
	if err != nil {
		t.Fatalf("sign state: %v", err)
	}

	expired, err := signStateWithExpiry("user-123", time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("sign expired state: %v", err)
	}

	tampered := valid[:len(valid)-1] + "x"

	tests := []struct {
		name    string
		state   string
		wantErr error
	}{
		{name: "valid state", state: valid, wantErr: nil},
		{name: "tampered signature", state: tampered, wantErr: ErrStateInvalid},
		{name: "expired state", state: expired, wantErr: ErrStateExpired},
		{name: "malformed: too few parts", state: "only.two.parts", wantErr: ErrStateInvalid},
		{name: "malformed: empty string", state: "", wantErr: ErrStateInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := VerifyState(tt.state)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestVerifyStateMissingSecretKey(t *testing.T) {
	t.Setenv("SECRET_KEY", "")

	if _, err := SignState("user-123"); err == nil {
		t.Fatal("expected error when SECRET_KEY is unset")
	}
	if _, err := VerifyState("a.b.c.d"); err == nil {
		t.Fatal("expected error when SECRET_KEY is unset")
	}
}

// signStateWithExpiry mirrors SignState's construction but embeds an
// arbitrary expiry, to deterministically produce an already-expired token
// for the expiry test case above without sleeping past the real stateTTL.
func signStateWithExpiry(userID string, expiresAt time.Time) (string, error) {
	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
	if len(secretKey) != 32 {
		return "", errors.New("gitprovider: SECRET_KEY must be exactly 32 bytes")
	}

	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	var expBuf [8]byte
	binary.BigEndian.PutUint64(expBuf[:], uint64(expiresAt.Unix()))

	payload := strings.Join([]string{
		base64.RawURLEncoding.EncodeToString(nonce),
		base64.RawURLEncoding.EncodeToString(expBuf[:]),
		userID,
	}, ".")

	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return payload + "." + sig, nil
}
