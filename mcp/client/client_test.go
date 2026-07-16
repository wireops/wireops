package client

import (
	"context"
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
