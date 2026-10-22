package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
		{name: "plain id", in: "stack0000000001", want: "stack0000000001"},
		{name: "single quote", in: "stack0000000001' || status='success", want: `stack0000000001\' || status=\'success`},
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

func TestAPIErrorHintNamesTheMissingCapability(t *testing.T) {
	err := &APIError{
		StatusCode: http.StatusForbidden,
		Body:       `{"error":"permission denied","required_capability":"manage_settings","minimum_role":"admin","actor_role":"operator"}`,
	}
	hint := err.Hint()
	for _, want := range []string{"manage_settings", "admin", "operator", "whoami"} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint %q should mention %q", hint, want)
		}
	}
	// An admin-only route is unreachable with any API key at all, which is
	// worth saying outright rather than leaving the caller to retry.
	if !strings.Contains(hint, "No API key can hold an admin capability") {
		t.Errorf("hint %q should say an API key can never pass an admin route", hint)
	}
	if !strings.Contains(err.Error(), hint) {
		t.Errorf("Error() should carry the hint, got %q", err.Error())
	}
}

func TestAPIErrorHintFallsBackOnAnOlderServerBody(t *testing.T) {
	err := &APIError{StatusCode: http.StatusForbidden, Body: `{"error":"permission denied"}`}
	if hint := err.Hint(); !strings.Contains(hint, "whoami") {
		t.Errorf("expected a generic 403 hint, got %q", hint)
	}
}

func TestAPIErrorHintPerStatus(t *testing.T) {
	cases := map[int]string{
		http.StatusUnauthorized:       "api key",
		http.StatusNotFound:           "no such record",
		http.StatusServiceUnavailable: "worker is offline",
		http.StatusConflict:           "current state",
	}
	for status, want := range cases {
		err := &APIError{StatusCode: status, Body: "{}"}
		if got := strings.ToLower(err.Hint()); !strings.Contains(got, want) {
			t.Errorf("status %d: hint %q should mention %q", status, got, want)
		}
	}
	// A status with nothing useful to add stays silent rather than guessing.
	plain := &APIError{StatusCode: http.StatusBadRequest, Body: `{"error":"compose_path required"}`}
	if plain.Hint() != "" {
		t.Errorf("expected no hint for a 400, got %q", plain.Hint())
	}
	if strings.Contains(plain.Error(), "—") {
		t.Errorf("Error() should not append an empty hint: %q", plain.Error())
	}
}

func TestStreamReadsNamedEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("Accept = %q, want text/event-stream", got)
		}
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, ": keep-alive\n\n")
		fmt.Fprint(w, "data: [compose_up] pulling\n\n")
		fmt.Fprint(w, "event: status\ndata: {\"status\":\"running\"}\n\n")
		fmt.Fprint(w, "event: status\ndata: {\"status\":\"success\"}\n\n")
		fmt.Fprint(w, "data: never read\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	var opened int
	var events []StreamEvent
	err := New(srv.URL).Stream(context.Background(), "wireops_sk_test", "/stream", nil,
		func() { opened++ },
		func(event StreamEvent) bool {
			events = append(events, event)
			return event.Name == "status" && strings.Contains(event.Data, "success")
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// onOpen fires once the response is 2xx, before any event — a caller that
	// must subscribe before triggering the work depends on that.
	if opened != 1 {
		t.Errorf("onOpen called %d times, want 1", opened)
	}
	if len(events) != 3 {
		t.Fatalf("expected to stop after the third event, got %d: %+v", len(events), events)
	}
	if events[0].Name != "" || events[0].Data != "[compose_up] pulling" {
		t.Errorf("first event = %+v, want an unnamed data frame", events[0])
	}
	if events[1].Name != "status" || events[1].Data != `{"status":"running"}` {
		t.Errorf("second event = %+v", events[1])
	}
}

func TestStreamSurfacesANonSuccessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"permission denied","required_capability":"view_logs"}`))
	}))
	defer srv.Close()

	err := New(srv.URL).Stream(context.Background(), "wireops_sk_viewer", "/stream", nil, nil, func(StreamEvent) bool { return true })
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("expected a 403 APIError, got %v", err)
	}
}
