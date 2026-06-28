package envvars

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/secrets"
)

// LoadStack resolves the effective environment for a stack. Global variables
// are loaded first and stack-local variables override them by key.
func LoadStack(ctx context.Context, app core.App, registry *secrets.Registry, stackID string) ([]string, error) {
	values, err := loadEffective(ctx, app, registry, "stack_env_vars", "stack", stackID, "stack_global_env_vars")
	if err != nil {
		return nil, err
	}
	return sortedEnvList(values), nil
}

// LoadJob resolves the effective environment for a scheduled job. Global
// variables are loaded first and job-local variables override them by key.
func LoadJob(ctx context.Context, app core.App, registry *secrets.Registry, jobID string) (map[string]string, error) {
	return loadEffective(ctx, app, registry, "job_env_vars", "job", jobID, "job_global_env_vars")
}

func loadEffective(ctx context.Context, app core.App, registry *secrets.Registry, localCollection, targetField, targetID, bindingCollection string) (map[string]string, error) {
	values := map[string]string{}

	globalRecords, err := loadGlobalRecords(app, bindingCollection, targetField, targetID)
	if err != nil {
		return nil, err
	}
	for _, rec := range globalRecords {
		if err := putResolved(ctx, registry, rec, values); err != nil {
			return nil, err
		}
	}

	localRecords, err := findAllIfCollectionExists(app, localCollection, dbx.HashExp{targetField: targetID})
	if err != nil {
		return nil, err
	}
	for _, rec := range localRecords {
		if err := putResolved(ctx, registry, rec, values); err != nil {
			return nil, err
		}
	}

	return values, nil
}

func loadGlobalRecords(app core.App, bindingCollection, targetField, targetID string) ([]*core.Record, error) {
	bindings, err := findAllIfCollectionExists(app, bindingCollection, dbx.HashExp{targetField: targetID})
	if err != nil {
		return nil, err
	}

	records := make([]*core.Record, 0, len(bindings))
	for _, binding := range bindings {
		globalID := binding.GetString("global_env_var")
		if globalID == "" {
			continue
		}
		rec, err := app.FindRecordById("global_env_vars", globalID)
		if err != nil {
			return nil, fmt.Errorf("load global env var %s: %w", globalID, err)
		}
		records = append(records, rec)
	}
	return records, nil
}

func findAllIfCollectionExists(app core.App, collection string, exprs ...dbx.Expression) ([]*core.Record, error) {
	if _, err := app.FindCollectionByNameOrId(collection); err != nil {
		return nil, nil
	}
	return app.FindAllRecords(collection, exprs...)
}

func putResolved(ctx context.Context, registry *secrets.Registry, rec *core.Record, values map[string]string) error {
	key := strings.TrimSpace(rec.GetString("key"))
	if key == "" {
		return nil
	}

	value := rec.GetString("value")
	if rec.GetBool("secret") {
		if registry == nil {
			return fmt.Errorf("cannot resolve secret env var %q: secret registry is not configured", key)
		}
		provider, err := registry.Get(rec.GetString("secret_provider"))
		if err != nil {
			return fmt.Errorf("cannot resolve secret env var %q: %w", key, err)
		}
		resolved, err := provider.Resolve(ctx, value)
		if err != nil {
			return fmt.Errorf("cannot resolve secret env var %q: %w", key, err)
		}
		value = resolved
	}

	values[key] = value
	return nil
}

func sortedEnvList(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}
