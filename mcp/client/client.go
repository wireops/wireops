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
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
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
