package tools

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/wireops/wireops/mcp/client"
	"github.com/wireops/wireops/mcp/models"
)

// pocketBaseIDPattern matches a PocketBase record id: 15 lowercase
// alphanumerics. Stack/worker/repository/job names cannot be confused with one
// in practice (a name that happens to be exactly 15 such characters is treated
// as an id, which is why the error on a miss says so).
var pocketBaseIDPattern = regexp.MustCompile(`^[a-z0-9]{15}$`)

// resolveRef turns a caller-supplied reference into a record id. An id is
// passed through untouched; anything else is looked up by the collection's
// human-meaningful name field (confirmNameField, the same mapping the confirm
// guard uses).
//
// This exists because every id in wireops is an opaque PocketBase id while a
// human says "the billing stack is down". Without it an agent has to list a
// collection and grep it before it can act, on every single task.
func resolveRef(ctx context.Context, c *client.Client, apiKey, collection, ref string) (string, error) {
	// An empty ref resolves to empty rather than erroring: whether the id is
	// required is the route's call, and the server already answers that the
	// same way it did before any resolution existed.
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil
	}
	if pocketBaseIDPattern.MatchString(ref) {
		return ref, nil
	}

	field, ok := confirmNameField[collection]
	if !ok {
		return "", fmt.Errorf("no name field registered for collection %q", collection)
	}

	var out struct {
		Items []map[string]interface{} `json:"items"`
	}
	q := url.Values{
		"filter":  {fmt.Sprintf("%s='%s'", field, client.EscapeFilterValue(ref))},
		"fields":  {"id," + field},
		"perPage": {"50"},
	}
	path := "/api/collections/" + url.PathEscape(collection) + "/records"
	if err := c.Get(ctx, apiKey, path, q, &out); err != nil {
		return "", fmt.Errorf("resolving %s %q: %w", singularCollection(collection), ref, err)
	}

	switch len(out.Items) {
	case 0:
		return "", fmt.Errorf("no %s has %s %q (and %q is not a wireops record id)", singularCollection(collection), field, ref, ref)
	case 1:
		id, _ := out.Items[0]["id"].(string)
		if id == "" {
			return "", fmt.Errorf("%s %q resolved to a record without an id", singularCollection(collection), ref)
		}
		return id, nil
	default:
		// Names are not unique in wireops, so an ambiguous reference is
		// reported with every candidate rather than resolved by guessing.
		candidates := make([]string, 0, len(out.Items))
		for _, item := range out.Items {
			id, _ := item["id"].(string)
			name, _ := item[field].(string)
			candidates = append(candidates, fmt.Sprintf("%s (%s)", id, name))
		}
		return "", fmt.Errorf("%s %s %q is ambiguous — %d records share it: %s. Pass one of those ids",
			singularCollection(collection), field, ref, len(out.Items), strings.Join(candidates, ", "))
	}
}

// singularCollection names a collection the way an error message should.
func singularCollection(collection string) string {
	switch collection {
	case confirmStacks:
		return "stack"
	case confirmWorkers:
		return "worker"
	case confirmRepos:
		return "repository"
	case confirmSchedJobs:
		return "scheduled job"
	default:
		return collection
	}
}

// Thin wrappers, so a handler reads as what it resolves rather than as a
// collection name.
func resolveStackID(ctx context.Context, c *client.Client, apiKey, ref string) (string, error) {
	return resolveRef(ctx, c, apiKey, confirmStacks, ref)
}

// resolveStackInput applies the documented stack -> stack_id alias before the
// normal id/name lookup. Keeping it here makes every tool retain the same
// ambiguity and RBAC-aware resolution behaviour.
func resolveStackInput(ctx context.Context, c *client.Client, apiKey, stackID, stack string) (string, error) {
	ref, err := models.CanonicalOrAlias(stackID, stack, paramStackID, paramStack)
	if err != nil {
		return "", err
	}
	if ref == "" {
		return "", fmt.Errorf("stack_id or stack is required")
	}
	return resolveStackID(ctx, c, apiKey, ref)
}

func resolveWorkerID(ctx context.Context, c *client.Client, apiKey, ref string) (string, error) {
	return resolveRef(ctx, c, apiKey, confirmWorkers, ref)
}

func resolveRepositoryID(ctx context.Context, c *client.Client, apiKey, ref string) (string, error) {
	return resolveRef(ctx, c, apiKey, confirmRepos, ref)
}

func resolveJobID(ctx context.Context, c *client.Client, apiKey, ref string) (string, error) {
	return resolveRef(ctx, c, apiKey, confirmSchedJobs, ref)
}
