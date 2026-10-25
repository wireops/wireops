// Package client is a thin REST client for the wireops server's existing
// API. It never holds a credential itself — every call takes the caller's
// API key explicitly and forwards it as-is (pass-through auth).
// Authorization is enforced entirely server-side by
// internal/auth.APIKeyMiddleware and internal/rbac, so a viewer-scoped key
// simply gets a 403 back from the server for the write verbs below.
package client

import (
	"bufio"
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
	if hint := e.Hint(); hint != "" {
		return fmt.Sprintf("wireops API error: status=%d body=%s — %s", e.StatusCode, e.Body, hint)
	}
	return fmt.Sprintf("wireops API error: status=%d body=%s", e.StatusCode, e.Body)
}

// forbiddenBody is the shape rbac.Require returns on a 403: the human message
// plus which capability was actually missing.
type forbiddenBody struct {
	Error              string `json:"error"`
	RequiredCapability string `json:"required_capability"`
	MinimumRole        string `json:"minimum_role"`
	ActorRole          string `json:"actor_role"`
}

// Hint turns a status code into the next step a caller can actually take.
// A bare `status=403 body={"error":"permission denied"}` tells an agent
// nothing about *why*, and the most common causes (a viewer-scoped key, an
// offline worker, an id that was really a name) each have a different remedy.
// Returns "" when there is nothing useful to add.
func (e *APIError) Hint() string {
	switch e.StatusCode {
	case http.StatusUnauthorized:
		return "no valid wireops API key was sent; check the key is not revoked or expired"
	case http.StatusForbidden:
		var body forbiddenBody
		if err := json.Unmarshal([]byte(e.Body), &body); err == nil && body.RequiredCapability != "" {
			hint := fmt.Sprintf("this key lacks the %q capability (minimum role %q", body.RequiredCapability, body.MinimumRole)
			if body.ActorRole != "" {
				hint += fmt.Sprintf(", this key is %q", body.ActorRole)
			}
			hint += "); call whoami to see what it can do"
			if body.MinimumRole == "admin" {
				// Service accounts cap at operator, so no API key will ever
				// pass this route however it is configured.
				hint += ". No API key can hold an admin capability — this needs a user session in the wireops UI"
			}
			return hint
		}
		return "this key's role does not allow this call; call whoami to see what it can do"
	case http.StatusNotFound:
		return "no such record — ids are wireops record ids; the name-accepting inputs resolve a name for you, so pass the exact name if you do not have the id"
	case http.StatusConflict:
		return "the server refused this in the resource's current state; the body above says which precondition failed"
	case http.StatusServiceUnavailable:
		return "most often the stack's worker is offline — check list_workers"
	}
	return ""
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

// StreamEvent is one server-sent event: its name ("" for an unnamed data
// frame) and its data payload.
type StreamEvent struct {
	Name string
	Data string
}

// Stream opens an authenticated SSE connection and calls onEvent for every
// event until onEvent returns true, the stream ends, or ctx is done. It is
// used to watch a reconcile to its conclusion instead of polling for it.
//
// onOpen (optional) is called once the server has answered 2xx, before any
// event arrives. A caller that has to subscribe before it triggers the work it
// is waiting for needs that signal: the first *event* can be arbitrarily far
// in the future, and may be the very one it would have missed.
//
// The shared http.Client's 30s timeout would cut a long watch short, so this
// uses its own transport-level settings and relies on ctx for the deadline.
func (c *Client) Stream(ctx context.Context, apiKey, path string, query url.Values, onOpen func(), onEvent func(StreamEvent) bool) error {
	reqURL := c.baseURL + path
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set(auth.APIKeyHeader, apiKey)
	req.Header.Set("Accept", "text/event-stream")

	streamClient := &http.Client{CheckRedirect: c.http.CheckRedirect}
	resp, err := streamClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling wireops API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	if onOpen != nil {
		onOpen()
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// An SSE event is a run of field lines terminated by a blank line; only
	// "event:" and "data:" matter here, and multi-line data is joined with \n
	// as the spec requires.
	var event StreamEvent
	var data []string
	flush := func() bool {
		if len(data) == 0 && event.Name == "" {
			return false
		}
		event.Data = strings.Join(data, "\n")
		stop := onEvent(event)
		event = StreamEvent{}
		data = nil
		return stop
	}

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		switch {
		case line == "":
			if flush() {
				return nil
			}
		case strings.HasPrefix(line, ":"):
			// Comment/keep-alive.
		case strings.HasPrefix(line, "event:"):
			event.Name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("reading event stream: %w", err)
	}
	flush()
	return ctx.Err()
}

// Post issues an authenticated POST with a JSON body.
func (c *Client) Post(ctx context.Context, apiKey, path string, query url.Values, body, out any) error {
	return c.do(ctx, apiKey, http.MethodPost, path, query, body, out)
}

// PostWithTimeout is for a request whose server-side contract legitimately
// exceeds the client's ordinary 30-second API timeout (currently one-shot
// container exec). It copies the configured redirect policy and otherwise
// uses the exact same authenticated request path as Post.
func (c *Client) PostWithTimeout(ctx context.Context, apiKey, path string, query url.Values, body, out any, timeout time.Duration) error {
	if timeout <= 0 {
		return c.Post(ctx, apiKey, path, query, body, out)
	}
	copyClient := *c
	copyHTTP := *c.http
	copyHTTP.Timeout = timeout
	copyClient.http = &copyHTTP
	return copyClient.do(ctx, apiKey, http.MethodPost, path, query, body, out)
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
