package tools

import (
	"context"
	"fmt"
	"net/url"

	"github.com/wireops/wireops/mcp/client"
)

// Collections and their human-meaningful name field, for requireConfirm.
const (
	confirmStacks    = "stacks"
	confirmWorkers   = "workers"
	confirmRepos     = "repositories"
	confirmSchedJobs = "scheduled_jobs"
)

var confirmNameField = map[string]string{
	confirmStacks:    "name",
	confirmWorkers:   "hostname",
	confirmRepos:     "name",
	confirmSchedJobs: "name",
}

// requireConfirm implements the "type the name to confirm" guard used by the
// irreversible tools. It resolves the record's own name server-side and
// demands that the caller repeated it exactly, so an agent cannot delete a
// stack it merely guessed the id of, and a human has to supply the name.
//
// The error names the expected value on purpose: the agent is meant to relay
// it and ask, not to retry on its own.
func requireConfirm(ctx context.Context, c *client.Client, apiKey, collection, id, got string) error {
	field, ok := confirmNameField[collection]
	if !ok {
		return fmt.Errorf("no confirm name field registered for collection %q", collection)
	}

	var rec map[string]interface{}
	path := "/api/collections/" + url.PathEscape(collection) + "/records/" + url.PathEscape(id)
	q := url.Values{"fields": {field}}
	if err := c.Get(ctx, apiKey, path, q, &rec); err != nil {
		return fmt.Errorf("resolving %s %q to confirm against: %w", collection, id, err)
	}
	expected, _ := rec[field].(string)
	if expected == "" {
		return fmt.Errorf("%s %q has no %s to confirm against", collection, id, field)
	}
	return matchConfirm(expected, got)
}

// matchConfirm compares a caller-supplied confirm value against the expected
// one, for callers that already know the expected string.
func matchConfirm(expected, got string) error {
	// An empty expected value would make an omitted confirm match it, turning
	// the guard off exactly when the caller resolved the target by something
	// other than its name (see resolveContainerTarget, which returns an empty
	// service name for a container with no compose service label).
	if expected == "" {
		return fmt.Errorf("no name to confirm against — refusing an irreversible action on a target that cannot be named")
	}
	if got == expected {
		return nil
	}
	if got == "" {
		return fmt.Errorf("this action is irreversible — set confirm to %q to proceed", expected)
	}
	return fmt.Errorf("confirm %q does not match %q — this action is irreversible, so it was not performed", got, expected)
}

// setIfNonEmpty adds key to body only when val is non-empty, so a PATCH built
// from an optional-fields input never blanks a field the caller left out.
func setIfNonEmpty(body map[string]any, key, val string) {
	if val != "" {
		body[key] = val
	}
}
