package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/wireops/wireops/mcp/client"
)

// Paging bounds shared by the collection-backed list tools. The cap exists
// because the response is a model's context: 200 full stack records is already
// more than any single question needs.
const (
	listDefaultPerPage = 50
	listMaxPerPage     = 200
)

// stackListFields is the default projection for list_stacks: enough to answer
// "what is deployed, is it healthy, and where", without the webhook secret,
// the resolved checksums or the import bookkeeping. render_overrides is
// included because hasRenderOverrides reads it, and sync_interval_seconds
// because the effective-interval annotation resolves it.
var stackListFields = []string{
	"id", "name", "status", "group",
	"repository", "worker",
	"compose_path", "compose_file",
	"current_version", "deployed_version", "deployed_commit",
	"last_synced_at", "sync_interval_seconds", "render_overrides",
	"last_error_category", "last_error_message", "last_error_at",
	"updated",
	// Expanded relations, so a listing names the repository and worker instead
	// of handing back two more ids to look up.
	"expand.repository.id", "expand.repository.name", "expand.repository.branch",
	"expand.repository.sync_interval_seconds",
	"expand.worker.id", "expand.worker.hostname", "expand.worker.status",
}

// repoListFields is the default projection for list_repos. Credentials live in
// a separate collection, so nothing here is sensitive — this is purely a size
// cut.
var repoListFields = []string{
	"id", "name", "git_url", "branch", "status",
	"last_commit_sha", "last_fetched_at", "sync_interval_seconds",
	"platform", "updated",
}

// paging converts a tool's limit/page into PocketBase query params.
func paging(limit, page int) url.Values {
	if limit <= 0 {
		limit = listDefaultPerPage
	}
	if limit > listMaxPerPage {
		limit = listMaxPerPage
	}
	if page <= 0 {
		page = 1
	}
	return url.Values{"perPage": {fmt.Sprint(limit)}, "page": {fmt.Sprint(page)}}
}

// fieldsParam renders a projection for the "fields" query param. A caller
// asking for "*" gets no projection at all, i.e. whole records.
func fieldsParam(requested, defaults []string) string {
	if len(requested) == 0 {
		return strings.Join(defaults, ",")
	}
	for _, field := range requested {
		if strings.TrimSpace(field) == "*" {
			return ""
		}
	}
	return strings.Join(requested, ",")
}

// filterClause is one PocketBase filter term.
type filterClause struct {
	field string
	op    string
	value string
}

// buildFilter joins the non-empty clauses with &&, escaping every value so a
// caller-supplied name cannot close the string literal and append its own
// clauses (see client.EscapeFilterValue).
func buildFilter(clauses []filterClause) string {
	terms := make([]string, 0, len(clauses))
	for _, clause := range clauses {
		if clause.value == "" {
			continue
		}
		terms = append(terms, fmt.Sprintf("%s%s'%s'", clause.field, clause.op, client.EscapeFilterValue(clause.value)))
	}
	return strings.Join(terms, " && ")
}

// scanPeriodSeconds reports the server's global SCAN_PERIOD, which is what a
// stack's or repository's sync_interval_seconds=0 actually resolves to. Read
// from the cached GET /api/custom/me; 0 means "unknown" (an older server
// without that route), and callers then omit the annotation rather than
// claiming a wrong interval.
func scanPeriodSeconds(ctx context.Context, c *client.Client, apiKey string) int {
	me, err := fetchWhoami(ctx, c, apiKey)
	if err != nil {
		return 0
	}
	seconds, ok := me["scan_period_seconds"].(float64)
	if !ok || seconds <= 0 {
		return 0
	}
	return int(seconds)
}

// annotateSyncInterval adds effective_sync_interval_seconds and its source to
// a stacks or repositories record. A stored 0 means "inherit", which the raw
// record does not say — the reviewer of this API could not tell whether a
// repository's 0 meant "disabled" or "inherits the global".
//
// The precedence mirrors internal/sync/scheduler.go: the record's own
// positive value wins, else the global SCAN_PERIOD. For a stack, the
// expanded repository's interval is reported as a sibling field rather than
// folded in, because the two schedule different things (the stack's reconcile
// tick vs. the repo's git fetch tick), not one overriding the other.
func annotateSyncInterval(rec map[string]interface{}, globalSeconds int) {
	own, _ := rec["sync_interval_seconds"].(float64)
	switch {
	case own > 0:
		rec["effective_sync_interval_seconds"] = int(own)
		rec["effective_sync_interval_source"] = "record"
	case globalSeconds > 0:
		rec["effective_sync_interval_seconds"] = globalSeconds
		rec["effective_sync_interval_source"] = "global SCAN_PERIOD (sync_interval_seconds=0 means inherit, not disabled)"
	}
}

// matchesSubstring reports whether value contains needle, case-insensitively.
// An empty needle matches everything, so a filter left unset is a no-op.
func matchesSubstring(value, needle string) bool {
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(value), strings.ToLower(needle))
}

// filterRecords narrows an array-returning custom route's response in-process.
// jobs/workers/job-groups routes answer with every record and take no query
// params, so the alternative is handing the whole fleet to the model on every
// call.
func filterRecords(items []interface{}, limit int, keep func(map[string]interface{}) bool) []interface{} {
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		rec, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if !keep(rec) {
			continue
		}
		out = append(out, item)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// recordString reads a string field from a decoded record, "" when absent.
func recordString(rec map[string]interface{}, field string) string {
	value, _ := rec[field].(string)
	return value
}
