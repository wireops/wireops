package envvars

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/secrets"
)

func TestLoadStackMergesGlobalsAndLocalOverrides(t *testing.T) {
	app := newEnvVarsTestApp(t)
	stack := mustCreateEnvRecord(t, app, "stacks", map[string]any{"name": "stack"})

	globalShared := mustCreateEnvRecord(t, app, "global_env_vars", map[string]any{
		"key":   "SHARED",
		"value": "global",
	})
	globalOnly := mustCreateEnvRecord(t, app, "global_env_vars", map[string]any{
		"key":   "GLOBAL_ONLY",
		"value": "global",
	})
	mustCreateEnvRecord(t, app, "stack_global_env_vars", map[string]any{"stack": stack.Id, "global_env_var": globalShared.Id})
	mustCreateEnvRecord(t, app, "stack_global_env_vars", map[string]any{"stack": stack.Id, "global_env_var": globalOnly.Id})
	mustCreateEnvRecord(t, app, "stack_env_vars", map[string]any{
		"stack": stack.Id,
		"key":   "SHARED",
		"value": "local",
	})
	mustCreateEnvRecord(t, app, "stack_env_vars", map[string]any{
		"stack": stack.Id,
		"key":   "LOCAL_ONLY",
		"value": "local",
	})

	got, err := LoadStack(context.Background(), app, secrets.NewDefaultRegistry([]byte(strings.Repeat("x", 32))), stack.Id)
	if err != nil {
		t.Fatalf("LoadStack failed: %v", err)
	}
	want := []string{"GLOBAL_ONLY=global", "LOCAL_ONLY=local", "SHARED=local"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadStack = %#v, want %#v", got, want)
	}
}

func TestLoadJobResolvesGlobalSecret(t *testing.T) {
	app := newEnvVarsTestApp(t)
	key := []byte(strings.Repeat("x", 32))
	job := mustCreateEnvRecord(t, app, "scheduled_jobs", map[string]any{"name": "job"})
	encrypted, err := crypto.Encrypt([]byte("s3cr3t"), key)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	globalSecret := mustCreateEnvRecord(t, app, "global_env_vars", map[string]any{
		"key":             "TOKEN",
		"value":           encrypted,
		"secret":          true,
		"secret_provider": "internal",
	})
	mustCreateEnvRecord(t, app, "job_global_env_vars", map[string]any{"job": job.Id, "global_env_var": globalSecret.Id})

	got, err := LoadJob(context.Background(), app, secrets.NewDefaultRegistry(key), job.Id)
	if err != nil {
		t.Fatalf("LoadJob failed: %v", err)
	}
	if got["TOKEN"] != "s3cr3t" {
		t.Fatalf("TOKEN = %q, want s3cr3t", got["TOKEN"])
	}
}

func newEnvVarsTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name"})
	mustSaveEnvCollection(t, app, stacks)

	jobs := core.NewBaseCollection("scheduled_jobs")
	jobs.Fields.Add(&core.TextField{Name: "name"})
	mustSaveEnvCollection(t, app, jobs)

	globals := core.NewBaseCollection("global_env_vars")
	globals.Fields.Add(&core.TextField{Name: "key"})
	globals.Fields.Add(&core.TextField{Name: "value"})
	globals.Fields.Add(&core.BoolField{Name: "secret"})
	globals.Fields.Add(&core.TextField{Name: "secret_provider"})
	mustSaveEnvCollection(t, app, globals)

	stackEnv := core.NewBaseCollection("stack_env_vars")
	stackEnv.Fields.Add(&core.RelationField{Name: "stack", CollectionId: stacks.Id, MaxSelect: 1})
	stackEnv.Fields.Add(&core.TextField{Name: "key"})
	stackEnv.Fields.Add(&core.TextField{Name: "value"})
	stackEnv.Fields.Add(&core.BoolField{Name: "secret"})
	stackEnv.Fields.Add(&core.TextField{Name: "secret_provider"})
	mustSaveEnvCollection(t, app, stackEnv)

	jobEnv := core.NewBaseCollection("job_env_vars")
	jobEnv.Fields.Add(&core.RelationField{Name: "job", CollectionId: jobs.Id, MaxSelect: 1})
	jobEnv.Fields.Add(&core.TextField{Name: "key"})
	jobEnv.Fields.Add(&core.TextField{Name: "value"})
	jobEnv.Fields.Add(&core.BoolField{Name: "secret"})
	mustSaveEnvCollection(t, app, jobEnv)

	stackGlobals := core.NewBaseCollection("stack_global_env_vars")
	stackGlobals.Fields.Add(&core.RelationField{Name: "stack", CollectionId: stacks.Id, MaxSelect: 1})
	stackGlobals.Fields.Add(&core.RelationField{Name: "global_env_var", CollectionId: globals.Id, MaxSelect: 1})
	mustSaveEnvCollection(t, app, stackGlobals)

	jobGlobals := core.NewBaseCollection("job_global_env_vars")
	jobGlobals.Fields.Add(&core.RelationField{Name: "job", CollectionId: jobs.Id, MaxSelect: 1})
	jobGlobals.Fields.Add(&core.RelationField{Name: "global_env_var", CollectionId: globals.Id, MaxSelect: 1})
	mustSaveEnvCollection(t, app, jobGlobals)

	return app
}

func mustSaveEnvCollection(t *testing.T, app core.App, col *core.Collection) {
	t.Helper()
	if err := app.Save(col); err != nil {
		t.Fatalf("save collection %s: %v", col.Name, err)
	}
}

func mustCreateEnvRecord(t *testing.T, app core.App, collection string, values map[string]any) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId(collection)
	if err != nil {
		t.Fatalf("find collection %s: %v", collection, err)
	}
	rec := core.NewRecord(col)
	for key, value := range values {
		rec.Set(key, value)
	}
	if err := app.Save(rec); err != nil {
		t.Fatalf("save record %s: %v", collection, err)
	}
	return rec
}
