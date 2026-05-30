package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	wiretls "github.com/wireops/wireops/pkg/tls"
)

var ErrRevoked = errors.New("worker token is revoked by the server")
var ErrUnauthorized = errors.New("worker token is invalid or expired")

// NewClient returns an HTTP client. TLS behaviour (e.g. skip-verify for
// self-signed certs) is controlled via the WORKER_TLS_SKIP_VERIFY environment
// variable, handled centrally by pkg/tls.
func NewClient() *http.Client {
	transport := http.DefaultTransport
	if tlsCfg := wiretls.BuildClientTLSConfig(); tlsCfg != nil {
		transport = &http.Transport{TLSClientConfig: tlsCfg}
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: transport}
}


func authHeaders(token string) http.Header {
	headers := make(http.Header)
	headers.Set("Content-Type", "application/json")
	headers.Set("X-Wireops-Worker-Token", strings.TrimSpace(token))
	return headers
}

func Register(client *http.Client, serverURL, token, hostname, version string, tags []string) error {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"hostname":   hostname,
		"ip_address": "",
		"version":    version,
		"tags":       tags,
	})

	serverURL = strings.TrimSuffix(serverURL, "/")
	req, err := http.NewRequest(http.MethodPost, serverURL+"/worker/register", bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}
	req.Header = authHeaders(token)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("registration request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return ErrRevoked
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("registration failed with status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("[worker] registered")
	return nil
}
