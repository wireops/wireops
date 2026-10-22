package tools

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wireops/wireops/mcp/client"
)

func TestPagingDefaultsAndClamps(t *testing.T) {
	cases := []struct {
		name            string
		limit, page     int
		perPage, number string
	}{
		{name: "unset", limit: 0, page: 0, perPage: "50", number: "1"},
		{name: "negative", limit: -5, page: -2, perPage: "50", number: "1"},
		{name: "explicit", limit: 10, page: 3, perPage: "10", number: "3"},
		{name: "over the cap", limit: 5000, page: 1, perPage: "200", number: "1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := paging(tc.limit, tc.page)
			if got := q.Get("perPage"); got != tc.perPage {
				t.Errorf("perPage = %q, want %q", got, tc.perPage)
			}
			if got := q.Get("page"); got != tc.number {
				t.Errorf("page = %q, want %q", got, tc.number)
			}
		})
	}
}

func TestFieldsParam(t *testing.T) {
	defaults := []string{"id", "name"}

	if got := fieldsParam(nil, defaults); got != "id,name" {
		t.Errorf("no request should use the default projection, got %q", got)
	}
	if got := fieldsParam([]string{"id", "status"}, defaults); got != "id,status" {
		t.Errorf("an explicit projection should be used verbatim, got %q", got)
	}
	// "*" means whole records, which PocketBase expresses as no fields param
	// at all — an empty string is the signal not to send one.
	if got := fieldsParam([]string{"*"}, defaults); got != "" {
		t.Errorf(`"*" should disable the projection, got %q`, got)
	}
	if got := fieldsParam([]string{"id", " * "}, defaults); got != "" {
		t.Errorf(`a padded "*" anywhere should disable the projection, got %q`, got)
	}
}

func TestBuildFilter(t *testing.T) {
	if got := buildFilter(nil); got != "" {
		t.Errorf("no clauses should produce no filter, got %q", got)
	}
	// An empty value is a filter the caller did not set, not a filter for the
	// empty string — sending it would match nothing.
	if got := buildFilter([]filterClause{{field: "status", op: "=", value: ""}}); got != "" {
		t.Errorf("an empty value should be skipped, got %q", got)
	}

	got := buildFilter([]filterClause{
		{field: "status", op: "=", value: "active"},
		{field: "name", op: "~", value: "bill"},
		{field: "worker", op: "=", value: ""},
	})
	if got != "status='active' && name~'bill'" {
		t.Errorf("unexpected filter %q", got)
	}

	// Values reach a PocketBase filter expression, so a quote must not be able
	// to close the literal and append clauses.
	got = buildFilter([]filterClause{{field: "name", op: "=", value: `a' || id!='`}})
	if got != `name='a\' || id!=\''` {
		t.Errorf("expected the quote escaped, got %q", got)
	}
}

func TestAnnotateSyncInterval(t *testing.T) {
	t.Run("own value wins", func(t *testing.T) {
		rec := map[string]interface{}{"sync_interval_seconds": float64(45)}
		annotateSyncInterval(rec, 10)
		if rec["effective_sync_interval_seconds"] != 45 {
			t.Errorf("effective = %v, want 45", rec["effective_sync_interval_seconds"])
		}
		if rec["effective_sync_interval_source"] != "record" {
			t.Errorf("source = %v, want record", rec["effective_sync_interval_source"])
		}
	})

	t.Run("zero inherits the global", func(t *testing.T) {
		// This is the whole point of the annotation: a stored 0 means
		// "inherit SCAN_PERIOD", which the raw record cannot say, and the
		// reviewer of this API read it as "disabled".
		rec := map[string]interface{}{"sync_interval_seconds": float64(0)}
		annotateSyncInterval(rec, 10)
		if rec["effective_sync_interval_seconds"] != 10 {
			t.Errorf("effective = %v, want 10", rec["effective_sync_interval_seconds"])
		}
		source, _ := rec["effective_sync_interval_source"].(string)
		if source == "record" || source == "" {
			t.Errorf("source = %q, want the global explanation", source)
		}
	})

	t.Run("unknown global annotates nothing", func(t *testing.T) {
		// An older server without /api/custom/me leaves the global unknown;
		// claiming an interval would be worse than omitting it.
		rec := map[string]interface{}{"sync_interval_seconds": float64(0)}
		annotateSyncInterval(rec, 0)
		if _, present := rec["effective_sync_interval_seconds"]; present {
			t.Errorf("expected no annotation, got %#v", rec)
		}
	})

	t.Run("missing field behaves like zero", func(t *testing.T) {
		rec := map[string]interface{}{}
		annotateSyncInterval(rec, 30)
		if rec["effective_sync_interval_seconds"] != 30 {
			t.Errorf("effective = %v, want 30", rec["effective_sync_interval_seconds"])
		}
	})
}

func TestMatchesSubstring(t *testing.T) {
	if !matchesSubstring("anything", "") {
		t.Error("an unset filter must match everything")
	}
	if !matchesSubstring("BillingAPI", "bill") {
		t.Error("expected a case-insensitive match")
	}
	if matchesSubstring("billing", "api") {
		t.Error("expected no match")
	}
}

func TestFilterRecords(t *testing.T) {
	items := []interface{}{
		map[string]interface{}{"name": "api"},
		"not-a-record",
		map[string]interface{}{"name": "billing"},
		map[string]interface{}{"name": "billing-worker"},
	}

	all := filterRecords(items, 0, func(map[string]interface{}) bool { return true })
	if len(all) != 3 {
		t.Fatalf("expected the three records and no junk entry, got %d", len(all))
	}

	matched := filterRecords(items, 0, func(rec map[string]interface{}) bool {
		return matchesSubstring(recordString(rec, "name"), "billing")
	})
	if len(matched) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matched))
	}

	limited := filterRecords(items, 1, func(map[string]interface{}) bool { return true })
	if len(limited) != 1 {
		t.Fatalf("expected the limit to apply, got %d", len(limited))
	}
}

func TestRecordString(t *testing.T) {
	rec := map[string]interface{}{"name": "api", "count": float64(3)}
	if got := recordString(rec, "name"); got != "api" {
		t.Errorf("name = %q", got)
	}
	// A non-string field reads as empty rather than panicking, so a filter
	// over an unexpected shape cannot take the process down.
	if got := recordString(rec, "count"); got != "" {
		t.Errorf("count = %q, want empty", got)
	}
	if got := recordString(rec, "absent"); got != "" {
		t.Errorf("absent = %q, want empty", got)
	}
}

func TestSingularCollection(t *testing.T) {
	cases := map[string]string{
		confirmStacks:    "stack",
		confirmWorkers:   "worker",
		confirmRepos:     "repository",
		confirmSchedJobs: "scheduled job",
		"other_things":   "other_things",
	}
	for collection, want := range cases {
		if got := singularCollection(collection); got != want {
			t.Errorf("singularCollection(%q) = %q, want %q", collection, got, want)
		}
	}
}

func TestTTLCache(t *testing.T) {
	cache := newTTLCache(time.Minute)

	if _, ok := cache.get("key-a"); ok {
		t.Error("expected a miss on an empty cache")
	}

	cache.put("key-a", map[string]interface{}{"role": "operator"})
	value, ok := cache.get("key-a")
	if !ok || value["role"] != "operator" {
		t.Fatalf("expected the cached value, got %#v (ok=%v)", value, ok)
	}

	// Entries are per API key: the value names the caller's own permissions,
	// so one key's entry must never answer for another.
	if _, ok := cache.get("key-b"); ok {
		t.Error("another key must not hit this entry")
	}
}

func TestTTLCacheExpiresAndSweeps(t *testing.T) {
	cache := newTTLCache(10 * time.Millisecond)
	cache.put("stale", map[string]interface{}{"role": "viewer"})

	time.Sleep(20 * time.Millisecond)
	if _, ok := cache.get("stale"); ok {
		t.Fatal("expected the entry to have expired")
	}

	// The next write sweeps what expired, so a long-lived process does not
	// hold an entry for a credential that stopped calling.
	cache.put("fresh", map[string]interface{}{"role": "operator"})
	cache.mu.Lock()
	_, stalePresent := cache.entries["stale"]
	count := len(cache.entries)
	cache.mu.Unlock()
	if stalePresent || count != 1 {
		t.Fatalf("expected the expired entry swept, got %d entries (stale present=%v)", count, stalePresent)
	}
}

func TestScanPeriodSecondsFallsBackToUnknown(t *testing.T) {
	cases := []struct {
		name string
		body string
		code int
		want int
	}{
		{name: "reported", body: `{"scan_period_seconds":45}`, want: 45},
		{name: "absent", body: `{"role":"operator"}`, want: 0},
		{name: "non-positive", body: `{"scan_period_seconds":0}`, want: 0},
		{name: "route missing", code: http.StatusNotFound, body: `{}`, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.code != 0 {
					w.WriteHeader(tc.code)
				}
				writeFixture(w, tc.body)
			}))
			defer srv.Close()

			// A distinct key per case, since fetchWhoami caches per key.
			apiKey := "wireops_sk_" + tc.name
			// 0 means "unknown", and annotateSyncInterval then omits the
			// annotation rather than claiming a wrong interval — an older
			// server without /api/custom/me must not produce a wrong number.
			if got := scanPeriodSeconds(ctxWithKey(), client.New(srv.URL), apiKey); got != tc.want {
				t.Fatalf("scanPeriodSeconds = %d, want %d", got, tc.want)
			}
		})
	}
}
