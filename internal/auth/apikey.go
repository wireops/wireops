package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/wireops/wireops/internal/crypto"
)

const lastUsedThrottle = 5 * time.Minute

const (
	APIKeyHeader      = "X-Wireops-Api-Key"
	APIKeyOrigin      = "api_key"
	apiKeyPrefix      = "wireops_sk_"
	apiKeyHashPrefix  = "hmac-sha256:"
	apiKeyHashContext = "wireops-api-key-hash-v1"
)

func APIKeyMiddleware(app core.App) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if e.Auth != nil {
			return e.Next()
		}

		raw := apiKeyFromRequest(e.Request)
		if raw == "" {
			return e.Next()
		}

		keyRecord, accountRecord, ok, err := authenticateAPIKey(app, raw)
		if err != nil {
			log.Printf("[auth] api key authentication failed due to invalid server configuration: %v", err)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "api key authentication is not configured"})
		}
		if !ok {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid api key"})
		}

		e.Auth = accountRecord
		e.Request.Header.Set("X-Wireops-Origin", APIKeyOrigin)

		lastUsed := keyRecord.GetDateTime("key_last_used_at")
		if lastUsed.IsZero() || time.Since(lastUsed.Time()) > lastUsedThrottle {
			keyRecord.Set("key_last_used_at", types.NowDateTime())
			if err := app.Save(keyRecord); err != nil {
				log.Printf("[auth] failed to update api key last_used_at for service account %s: %v", keyRecord.Id, err)
			}
		}

		return e.Next()
	}
}

func GenerateAPIKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return apiKeyPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func HashAPIKey(key string) (string, error) {
	secret, err := apiKeyHashSecret()
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(strings.TrimSpace(key)))
	return apiKeyHashPrefix + hex.EncodeToString(mac.Sum(nil)), nil
}

func apiKeyHashSecret() ([]byte, error) {
	raw := os.Getenv("SECRET_KEY")
	if err := crypto.ValidateSecretKey(raw); err != nil {
		return nil, err
	}
	secretKey := crypto.NormalizeSecretKey(raw)
	return append([]byte(apiKeyHashContext+":"), secretKey...), nil
}

func APIKeyPrefix(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 16 {
		return key
	}
	return key[:16]
}

func apiKeyFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if key := strings.TrimSpace(r.Header.Get(APIKeyHeader)); key != "" {
		return key
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer "+apiKeyPrefix) {
		return strings.TrimSpace(auth[len("bearer "):])
	}
	return ""
}

func authenticateAPIKey(app core.App, raw string) (*core.Record, *core.Record, bool, error) {
	keyHash, err := HashAPIKey(raw)
	if err != nil {
		return nil, nil, false, err
	}
	records, err := app.FindAllRecords("service_accounts", dbx.HashExp{"key_hash": keyHash})
	if err != nil || len(records) == 0 {
		return nil, nil, false, nil
	}
	accountRecord := records[0]
	if !accountRecord.GetBool("enabled") {
		return nil, nil, false, nil
	}
	if accountRecord.GetBool("key_revoked") {
		return nil, nil, false, nil
	}
	expiresAt := accountRecord.GetDateTime("key_expires_at")
	if !expiresAt.IsZero() && expiresAt.Time().Before(time.Now()) {
		return nil, nil, false, nil
	}
	return accountRecord, accountRecord, true, nil
}
