// Package client is a thin REST client for the wireops server's existing
// API. It never holds a credential itself — every call takes the caller's
// API key explicitly and forwards it as-is (pass-through auth).
// Authorization is enforced entirely server-side by
// internal/auth.APIKeyMiddleware and internal/rbac.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wireops/wireops/internal/auth"
)

// Client calls the wireops server REST API on behalf of an MCP tool caller.
type Client struct {
	baseURL string
	http    *http.Client
}

// New creates a Client targeting the given wireops server base URL.
func New(baseURL string) *Client {
	trimmed := strings.TrimRight(baseURL, "/")
	origin, _ := url.Parse(trimmed)

	return &Client{
		baseURL: trimmed,
		http: &http.Client{
			Timeout: 30 * time.Second,
			// Redirects normally carry forward custom request headers, so a
			// redirect to another origin would leak X-Wireops-Api-Key to it.
			// Only permit redirects that stay on the configured server's origin.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if origin == nil || req.URL.Scheme != origin.Scheme || req.URL.Host != origin.Host {
					return fmt.Errorf("refusing cross-origin redirect to %s", req.URL)
				}
				if len(via) >= 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				return nil
			},
		},
	}
}

// EscapeFilterValue escapes v for safe interpolation as a single-quoted
// string literal in a PocketBase filter expression (e.g. fmt.Sprintf("stack='%s'",
// client.EscapeFilterValue(stackID))). PocketBase's fexpr grammar treats a
// backslash as the escape character for the matching quote, so a caller-
// supplied value containing a bare "'" could otherwise close the literal
// early and inject additional filter clauses.
func EscapeFilterValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `'`, `\'`)
	return v
}

// APIError is returned when the wireops server responds with a non-2xx status.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("wireops API error: status=%d body=%s", e.StatusCode, e.Body)
}

// Get issues an authenticated GET against path (e.g. "/api/collections/stacks/records")
// with the given query params, and decodes the JSON response into out.
func (c *Client) Get(ctx context.Context, apiKey, path string, query url.Values, out any) error {
	reqURL := c.baseURL + path
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set(auth.APIKeyHeader, apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("calling wireops API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}
