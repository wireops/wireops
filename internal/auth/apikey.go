package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

const lastUsedThrottle = 5 * time.Minute

const (
	APIKeyHeader = "X-Wireops-Api-Key"
	APIKeyOrigin = "api_key"
	apiKeyPrefix = "wireops_sk_"
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

		keyRecord, accountRecord, ok := authenticateAPIKey(app, raw)
		if !ok {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid api key"})
		}

		e.Auth = accountRecord
		e.Request.Header.Set("X-Wireops-Origin", APIKeyOrigin)

		lastUsed := keyRecord.GetDateTime("last_used_at")
		if lastUsed.IsZero() || time.Since(lastUsed.Time()) > lastUsedThrottle {
			keyRecord.Set("last_used_at", types.NowDateTime())
			if err := app.Save(keyRecord); err != nil {
				log.Printf("[auth] failed to update api key last_used_at for key %s: %v", keyRecord.Id, err)
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

func HashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])
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

func authenticateAPIKey(app core.App, raw string) (*core.Record, *core.Record, bool) {
	records, err := app.FindAllRecords("api_keys", dbx.HashExp{"key_hash": HashAPIKey(raw)})
	if err != nil || len(records) == 0 {
		return nil, nil, false
	}
	keyRecord := records[0]
	if keyRecord.GetBool("revoked") {
		return nil, nil, false
	}
	expiresAt := keyRecord.GetDateTime("expires_at")
	if !expiresAt.IsZero() && expiresAt.Time().Before(time.Now()) {
		return nil, nil, false
	}

	accountID := keyRecord.GetString("service_account")
	accountRecord, err := app.FindRecordById("service_accounts", accountID)
	if err != nil || !accountRecord.GetBool("enabled") {
		return nil, nil, false
	}
	return keyRecord, accountRecord, true
}
