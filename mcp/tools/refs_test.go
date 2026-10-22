package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wireops/wireops/mcp/client"
	"github.com/wireops/wireops/mcp/models"
)

func TestResolveRefPassesThroughARecordID(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	got, err := resolveStackID(ctxWithKey(), client.New(srv.URL), "wireops_sk_test", "stack0000000001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "stack0000000001" {
		t.Fatalf("expected the id unchanged, got %q", got)
	}
	// An id costs no round trip — otherwise every tool call would pay for the
	// convenience of accepting names.
	if calls != 0 {
		t.Fatalf("expected no lookup request for an id, got %d", calls)
	}
}

func TestResolveRefEmptyStaysEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("expected no request for an empty ref")
	}))
	defer srv.Close()

	got, err := resolveStackID(ctxWithKey(), client.New(srv.URL), "wireops_sk_test", "  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestResolveRefLooksUpAName(t *testing.T) {
	var gotPath, gotFilter, gotFields string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotFilter = r.URL.Query().Get("filter")
		gotFields = r.URL.Query().Get("fields")
		w.Write([]byte(`{"items":[{"id":"stack0000000001","name":"billing"}]}`))
	}))
	defer srv.Close()

	got, err := resolveStackID(ctxWithKey(), client.New(srv.URL), "wireops_sk_test", "billing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "stack0000000001" {
		t.Fatalf("expected the resolved id, got %q", got)
	}
	if gotPath != "/api/collections/stacks/records" {
		t.Fatalf("unexpected path %q", gotPath)
	}
	if gotFilter != "name='billing'" {
		t.Fatalf("unexpected filter %q", gotFilter)
	}
	if gotFields != "id,name" {
		t.Fatalf("expected a projected lookup, got fields %q", gotFields)
	}
}

func TestResolveRefEscapesAQuoteInTheName(t *testing.T) {
	var gotFilter string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFilter = r.URL.Query().Get("filter")
		w.Write([]byte(`{"items":[{"id":"stack0000000001","name":"x"}]}`))
	}))
	defer srv.Close()

	// A name reaches a PocketBase filter expression, so a bare quote would
	// close the literal early and append clauses of the caller's choosing.
	if _, err := resolveStackID(ctxWithKey(), client.New(srv.URL), "wireops_sk_test", `a' || id!='`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotFilter != `name='a\' || id!=\''` {
		t.Fatalf("expected the quote escaped, got %q", gotFilter)
	}
}

func TestResolveRefReportsAMiss(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	_, err := resolveWorkerID(ctxWithKey(), client.New(srv.URL), "wireops_sk_test", "nope")
	if err == nil {
		t.Fatal("expected an error for an unknown name")
	}
	for _, want := range []string{"worker", "hostname", "nope"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestResolveRefRefusesAnAmbiguousName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[{"id":"stack0000000001","name":"api"},{"id":"stack0000000002","name":"api"}]}`))
	}))
	defer srv.Close()

	_, err := resolveStackID(ctxWithKey(), client.New(srv.URL), "wireops_sk_test", "api")
	if err == nil {
		t.Fatal("expected an error for an ambiguous name")
	}
	// Both candidates must be named, so the agent can relay the choice instead
	// of acting on one of two stacks at random.
	for _, want := range []string{"ambiguous", "stack0000000001", "stack0000000002"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestHandlersAcceptANameInPlaceOfAnID(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/api/collections/stacks/records" {
			w.Write([]byte(`{"items":[{"id":"stack0000000001","name":"billing"}]}`))
			return
		}
		w.Write([]byte(`{"id":"stack0000000001","status":"active"}`))
	}))
	defer srv.Close()

	handler := getStackStatus(client.New(srv.URL))
	if _, _, err := handler(ctxWithKey(), nil, models.StackIDInput{StackID: "billing"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "/api/collections/stacks/records/stack0000000001"
	for _, path := range paths {
		if path == want {
			return
		}
	}
	t.Fatalf("expected the resolved id in the record path, got %v", paths)
}
