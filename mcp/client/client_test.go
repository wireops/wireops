package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/wireops/wireops/internal/auth"
)

func TestGetForwardsAPIKeyAndDecodesJSON(t *testing.T) {
	var gotHeader string
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get(auth.APIKeyHeader)
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{"id":"abc"}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL)

	var out struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	err := c.Get(context.Background(), "wireops_sk_test", "/api/collections/stacks/records", url.Values{"perPage": {"50"}}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotHeader != "wireops_sk_test" {
		t.Fatalf("expected API key header forwarded, got %q", gotHeader)
	}
	if gotQuery.Get("perPage") != "50" {
		t.Fatalf("expected perPage query param forwarded, got %q", gotQuery.Get("perPage"))
	}
	if len(out.Items) != 1 || out.Items[0].ID != "abc" {
		t.Fatalf("unexpected decoded output: %+v", out)
	}
}

func TestGetReturnsAPIErrorOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)

	var out any
	err := c.Get(context.Background(), "wireops_sk_test", "/api/collections/stacks/records", nil, &out)
	if err == nil {
		t.Fatal("expected error on 403 response")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", apiErr.StatusCode)
	}
}

func TestGetRefusesCrossOriginRedirect(t *testing.T) {
	var attackerGotHeader string
	var attackerHit bool
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attackerHit = true
		attackerGotHeader = r.Header.Get(auth.APIKeyHeader)
		w.Write([]byte(`{}`))
	}))
	defer attacker.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, attacker.URL+"/steal", http.StatusFound)
	}))
	defer origin.Close()

	c := New(origin.URL)

	var out any
	err := c.Get(context.Background(), "wireops_sk_test", "/api/collections/stacks/records", nil, &out)
	if err == nil {
		t.Fatal("expected error on cross-origin redirect")
	}
	if attackerHit {
		t.Fatalf("attacker origin should never have been reached, but was hit (got header %q)", attackerGotHeader)
	}
	if attackerGotHeader != "" {
		t.Fatalf("API key leaked to cross-origin redirect target: %q", attackerGotHeader)
	}
}

func TestEscapeFilterValue(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain id", in: "stack1", want: "stack1"},
		{name: "single quote", in: "stack1' || status='success", want: `stack1\' || status=\'success`},
		{name: "backslash", in: `a\b`, want: `a\\b`},
		{name: "backslash before quote", in: `a\'b`, want: `a\\\'b`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EscapeFilterValue(tt.in); got != tt.want {
				t.Fatalf("EscapeFilterValue(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestGetWithNilOutSkipsDecode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not valid json`))
	}))
	defer srv.Close()

	c := New(srv.URL)

	err := c.Get(context.Background(), "wireops_sk_test", "/api/custom/stacks/x/services", nil, nil)
	if err != nil {
		t.Fatalf("expected no error when out is nil, got %v", err)
	}
}

// writeRecorder captures what a write verb actually put on the wire.
type writeRecorder struct {
	method      string
	path        string
	query       string
	contentType string
	apiKey      string
	body        string
}

func newWriteServer(t *testing.T, response string) (*httptest.Server, *writeRecorder) {
	t.Helper()
	rec := &writeRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.query = r.URL.RawQuery
		rec.contentType = r.Header.Get("Content-Type")
		rec.apiKey = r.Header.Get(auth.APIKeyHeader)
		rec.body = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func TestWriteVerbsSendMethodBodyAndHeaders(t *testing.T) {
	cases := []struct {
		name   string
		call   func(c *Client, ctx context.Context) error
		method string
		body   string
	}{
		{
			name: "post",
			call: func(c *Client, ctx context.Context) error {
				return c.Post(ctx, "wireops_sk_test", "/api/custom/stacks/s1/sync", nil, map[string]any{"a": 1}, nil)
			},
			method: http.MethodPost,
			body:   `{"a":1}`,
		},
		{
			name: "put",
			call: func(c *Client, ctx context.Context) error {
				return c.Put(ctx, "wireops_sk_test", "/api/custom/stacks/s1/render-overrides", nil, map[string]any{"a": 1}, nil)
			},
			method: http.MethodPut,
			body:   `{"a":1}`,
		},
		{
			name: "patch",
			call: func(c *Client, ctx context.Context) error {
				return c.Patch(ctx, "wireops_sk_test", "/api/collections/stacks/records/s1", nil, map[string]any{"a": 1}, nil)
			},
			method: http.MethodPatch,
			body:   `{"a":1}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, rec := newWriteServer(t, `{}`)
			if err := tc.call(New(srv.URL), context.Background()); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rec.method != tc.method {
				t.Fatalf("expected %s, got %s", tc.method, rec.method)
			}
			if rec.body != tc.body {
				t.Fatalf("expected body %s, got %s", tc.body, rec.body)
			}
			if rec.contentType != "application/json" {
				t.Fatalf("expected a JSON content type, got %q", rec.contentType)
			}
			if rec.apiKey != "wireops_sk_test" {
				t.Fatalf("expected the API key forwarded, got %q", rec.apiKey)
			}
		})
	}
}

func TestDeleteSendsNoBodyAndForwardsQuery(t *testing.T) {
	srv, rec := newWriteServer(t, `{}`)

	if err := New(srv.URL).Delete(context.Background(), "wireops_sk_test", "/api/custom/stacks/s1", url.Values{"force": {"true"}}, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.method != http.MethodDelete {
		t.Fatalf("expected DELETE, got %s", rec.method)
	}
	if rec.body != "" {
		t.Fatalf("expected no body, got %q", rec.body)
	}
	// Without a body there is nothing to describe, so the header must stay off.
	if rec.contentType != "" {
		t.Fatalf("expected no content type on a bodyless request, got %q", rec.contentType)
	}
	if rec.query != "force=true" {
		t.Fatalf("expected the query forwarded, got %q", rec.query)
	}
}

func TestPostDecodesResponseInto(t *testing.T) {
	srv, _ := newWriteServer(t, `{"session_id":"sess-1"}`)

	var out struct {
		SessionID string `json:"session_id"`
	}
	if err := New(srv.URL).Post(context.Background(), "wireops_sk_test", "/x", nil, struct{}{}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.SessionID != "sess-1" {
		t.Fatalf("expected the response decoded, got %+v", out)
	}
}

func TestWriteVerbsSurfaceAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"permission denied"}`))
	}))
	defer srv.Close()

	err := New(srv.URL).Post(context.Background(), "wireops_sk_viewer", "/api/custom/stacks/s1/sync", nil, struct{}{}, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected an APIError, got %v", err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", apiErr.StatusCode)
	}
}

func TestGetRawReturnsBodyVerbatim(t *testing.T) {
	ndjson := "{\"version\":2}\n[0,\"o\",\"aGk=\"]\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(ndjson))
	}))
	defer srv.Close()

	got, err := New(srv.URL).GetRaw(context.Background(), "wireops_sk_test", "/transcript", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != ndjson {
		t.Fatalf("expected the body verbatim, got %q", got)
	}
}

func TestGetRawSurfacesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := New(srv.URL).GetRaw(context.Background(), "wireops_sk_test", "/transcript", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("expected a 404 APIError, got %v", err)
	}
}
