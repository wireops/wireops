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
	"time"

	"github.com/wireops/wireops/internal/crypto"
)

// stateTTL bounds how long an OAuth authorize URL stays valid before its
// signed state token is rejected — long enough for a user to complete the
// provider's consent screen, short enough that a leaked/logged callback URL
// can't be replayed indefinitely.
const stateTTL = 10 * time.Minute

var (
	// ErrStateExpired is returned by VerifyState when the state token's
	// embedded expiry has passed.
	ErrStateExpired = errors.New("gitprovider: oauth state expired")
	// ErrStateInvalid is returned by VerifyState when the state token is
	// malformed or its HMAC does not match.
	ErrStateInvalid = errors.New("gitprovider: oauth state invalid")
)

// SignState produces a stateless, HMAC-signed opaque token embedding
// userID and an expiry, for use as the OAuth "state" query parameter. No
// server-side storage is needed — VerifyState re-derives and checks the
// HMAC against SECRET_KEY.
func SignState(userID string) (string, error) {
	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
	if len(secretKey) != 32 {
		return "", errors.New("gitprovider: SECRET_KEY must be exactly 32 bytes")
	}

	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	expiresAt := time.Now().Add(stateTTL).Unix()
	var expBuf [8]byte
	binary.BigEndian.PutUint64(expBuf[:], uint64(expiresAt))

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

// VerifyState validates a state token produced by SignState and returns the
// embedded userID.
func VerifyState(state string) (userID string, err error) {
	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
	if len(secretKey) != 32 {
		return "", errors.New("gitprovider: SECRET_KEY must be exactly 32 bytes")
	}

	parts := strings.SplitN(state, ".", 4)
	if len(parts) != 4 {
		return "", ErrStateInvalid
	}
	nonceB64, expB64, userID, sig := parts[0], parts[1], parts[2], parts[3]

	payload := nonceB64 + "." + expB64 + "." + userID
	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(payload))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return "", ErrStateInvalid
	}

	expBuf, err := base64.RawURLEncoding.DecodeString(expB64)
	if err != nil || len(expBuf) != 8 {
		return "", ErrStateInvalid
	}
	expiresAt := int64(binary.BigEndian.Uint64(expBuf))
	if time.Now().Unix() > expiresAt {
		return "", ErrStateExpired
	}

	return userID, nil
}
