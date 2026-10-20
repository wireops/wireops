// Package client is a thin REST client for the wireops server's existing
// API. It never holds a credential itself — every call takes the caller's
// API key explicitly and forwards it as-is (pass-through auth).
// Authorization is enforced entirely server-side by
// internal/auth.APIKeyMiddleware and internal/rbac, so a viewer-scoped key
// simply gets a 403 back from the server for the write verbs below.
package client

import (
	"bytes"
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

// do issues an authenticated request against path with the given query params
// and optional JSON body, decoding the JSON response into out (out may be nil
// to discard it). It is the single place where the API key header, the non-2xx
// handling and the JSON codec live; every verb below is a thin wrapper.
func (c *Client) do(ctx context.Context, apiKey, method, path string, query url.Values, body, out any) error {
	reqURL := c.baseURL + path
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}

	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
		// bytes.Reader (not bytes.Buffer) so net/http can replay the body on
		// a same-origin redirect instead of resending an empty one.
		payload = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, payload)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set(auth.APIKeyHeader, apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("calling wireops API: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}

// Get issues an authenticated GET against path (e.g. "/api/collections/stacks/records")
// with the given query params, and decodes the JSON response into out.
func (c *Client) Get(ctx context.Context, apiKey, path string, query url.Values, out any) error {
	return c.do(ctx, apiKey, http.MethodGet, path, query, nil, out)
}

// GetRaw issues an authenticated GET and returns the response body verbatim,
// for endpoints that don't answer with JSON (e.g. the NDJSON terminal
// transcript).
func (c *Client) GetRaw(ctx context.Context, apiKey, path string, query url.Values) ([]byte, error) {
	reqURL := c.baseURL + path
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set(auth.APIKeyHeader, apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling wireops API: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}
	return respBody, nil
}

// Post issues an authenticated POST with a JSON body.
func (c *Client) Post(ctx context.Context, apiKey, path string, query url.Values, body, out any) error {
	return c.do(ctx, apiKey, http.MethodPost, path, query, body, out)
}

// Put issues an authenticated PUT with a JSON body.
func (c *Client) Put(ctx context.Context, apiKey, path string, query url.Values, body, out any) error {
	return c.do(ctx, apiKey, http.MethodPut, path, query, body, out)
}

// Patch issues an authenticated PATCH with a JSON body.
func (c *Client) Patch(ctx context.Context, apiKey, path string, query url.Values, body, out any) error {
	return c.do(ctx, apiKey, http.MethodPatch, path, query, body, out)
}

// Delete issues an authenticated DELETE.
func (c *Client) Delete(ctx context.Context, apiKey, path string, query url.Values, out any) error {
	return c.do(ctx, apiKey, http.MethodDelete, path, query, nil, out)
}
