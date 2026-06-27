package dbcheck

import (
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func TestValidateHealthyDatabase(t *testing.T) {
	app := newDBCheckTestApp(t)

	result := Validate(app)

	if !result.OK {
		t.Fatalf("result.OK = false, issues: %#v", result.Issues)
	}
	if result.IssueCount != 0 {
		t.Fatalf("issue count = %d, want 0", result.IssueCount)
	}
	if result.Collections["repositories"] != 1 {
		t.Fatalf("repositories count = %d, want 1", result.Collections["repositories"])
	}
}

func TestValidateDetectsConsistencyErrors(t *testing.T) {
	app := newDBCheckTestApp(t)
	fixture := createConsistentFixture(t, app)

	if _, err := app.DB().NewQuery(`
		UPDATE scheduled_jobs
		SET repository = 'missing-repository', name = '', job_file = '../secret.yml'
		WHERE id = {:job}
	`).Bind(dbx.Params{"job": fixture.jobID}).Execute(); err != nil {
		t.Fatalf("failed to corrupt scheduled job: %v", err)
	}
	if _, err := app.DB().NewQuery(`
		UPDATE stacks
		SET repository = '', source_type = 'git', compose_file = '../compose.yml'
		WHERE id = {:stack}
	`).Bind(dbx.Params{"stack": fixture.stackID}).Execute(); err != nil {
		t.Fatalf("failed to corrupt stack: %v", err)
	}

	result := Validate(app)

	if result.OK {
		t.Fatal("result.OK = true, want false")
	}
	assertIssue(t, result, "relation_target_missing", "scheduled_jobs", "repository")
	assertIssue(t, result, "required_field_missing", "scheduled_jobs", "name")
	assertIssue(t, result, "unsafe_path", "scheduled_jobs", "job_file")
	assertIssue(t, result, "git_stack_repository_missing", "stacks", "repository")
	assertIssue(t, result, "unsafe_path", "stacks", "compose_file")
}

func newDBCheckTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })
	createDBCheckCollections(t, app)
	createConsistentFixture(t, app)
	return app
}

type dbCheckFixture struct {
	repoID   string
	workerID string
	stackID  string
	jobID    string
}

func createConsistentFixture(t *testing.T, app core.App) dbCheckFixture {
	t.Helper()
	if records, err := app.FindAllRecords("repositories"); err == nil && len(records) > 0 {
		stacks, _ := app.FindAllRecords("stacks")
		jobs, _ := app.FindAllRecords("scheduled_jobs")
		fixture := dbCheckFixture{repoID: records[0].Id}
		if workers, _ := app.FindAllRecords("workers"); len(workers) > 0 {
			fixture.workerID = workers[0].Id
		}
		if len(stacks) > 0 {
			fixture.stackID = stacks[0].Id
		}
		if len(jobs) > 0 {
			fixture.jobID = jobs[0].Id
		}
		return fixture
	}

	key := mustCreateDBCheckRecord(t, app, "repository_keys", map[string]any{"name": "GitHub", "auth_type": "basic"})
	repo := mustCreateDBCheckRecord(t, app, "repositories", map[string]any{
		"name":           "repo",
		"git_url":        "https://example.com/repo.git",
		"repository_key": key.Id,
	})
	worker := mustCreateDBCheckRecord(t, app, "workers", map[string]any{
		"hostname":    "worker-1",
		"fingerprint": "fingerprint-1",
		"status":      "ACTIVE",
	})
	stack := mustCreateDBCheckRecord(t, app, "stacks", map[string]any{
		"name":         "stack",
		"repository":   repo.Id,
		"worker":       worker.Id,
		"compose_path": "deploy",
		"compose_file": "compose.yml",
		"source_type":  "git",
	})
	job := mustCreateDBCheckRecord(t, app, "scheduled_jobs", map[string]any{
		"repository":  repo.Id,
		"name":        "Nightly Backup",
		"description": "Runs backups",
		"job_file":    "ops/job.yaml",
		"enabled":     true,
		"status":      "active",
	})

	mustCreateDBCheckRecord(t, app, "worker_tokens", map[string]any{"token_hash": "hash-1", "status": "ACTIVE", "worker": worker.Id})
	mustCreateDBCheckRecord(t, app, "worker_commands", map[string]any{"worker": worker.Id, "command_id": "cmd-1", "command_type": "deploy", "status": "success"})
	mustCreateDBCheckRecord(t, app, "stack_env_vars", map[string]any{"stack": stack.Id, "key": "APP_ENV", "value": "prod"})
	mustCreateDBCheckRecord(t, app, "stack_services", map[string]any{"stack": stack.Id, "service_name": "api"})
	mustCreateDBCheckRecord(t, app, "stack_revisions", map[string]any{"stack": stack.Id, "version": 1, "commit_sha": "abc", "checksum": "def", "compose_path": "deploy"})
	mustCreateDBCheckRecord(t, app, "stack_pending_reconciles", map[string]any{"stack": stack.Id, "trigger": "manual"})
	mustCreateDBCheckRecord(t, app, "sync_logs", map[string]any{"stack": stack.Id, "trigger": "manual", "status": "success"})
	mustCreateDBCheckRecord(t, app, "job_env_vars", map[string]any{"job": job.Id, "key": "BACKUP_PATH", "value": "/backup"})
	mustCreateDBCheckRecord(t, app, "job_runs", map[string]any{"job": job.Id, "worker": worker.Id, "trigger": "manual", "status": "success"})
	mustCreateDBCheckRecord(t, app, "worker_policies", map[string]any{"enabled": true})
	mustCreateDBCheckRecord(t, app, "integrations", map[string]any{"slug": "dozzle", "enabled": true})
	mustCreateDBCheckRecord(t, app, "invites", map[string]any{"email": "user@example.com", "token": "token"})

	return dbCheckFixture{repoID: repo.Id, workerID: worker.Id, stackID: stack.Id, jobID: job.Id}
}

func createDBCheckCollections(t *testing.T, app core.App) {
	t.Helper()

	keys := core.NewBaseCollection("repository_keys")
	keys.Fields.Add(&core.TextField{Name: "name"})
	keys.Fields.Add(&core.TextField{Name: "auth_type"})
	mustSaveDBCheckCollection(t, app, keys)

	repos := core.NewBaseCollection("repositories")
	repos.Fields.Add(&core.TextField{Name: "name"})
	repos.Fields.Add(&core.TextField{Name: "git_url"})
	repos.Fields.Add(&core.RelationField{Name: "repository_key", CollectionId: keys.Id, MaxSelect: 1})
	mustSaveDBCheckCollection(t, app, repos)

	workers := core.NewBaseCollection("workers")
	workers.Fields.Add(&core.TextField{Name: "hostname"})
	workers.Fields.Add(&core.TextField{Name: "fingerprint"})
	workers.Fields.Add(&core.TextField{Name: "status"})
	mustSaveDBCheckCollection(t, app, workers)

	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name"})
	stacks.Fields.Add(&core.RelationField{Name: "repository", CollectionId: repos.Id, MaxSelect: 1})
	stacks.Fields.Add(&core.RelationField{Name: "worker", CollectionId: workers.Id, MaxSelect: 1})
	stacks.Fields.Add(&core.TextField{Name: "compose_path"})
	stacks.Fields.Add(&core.TextField{Name: "compose_file"})
	stacks.Fields.Add(&core.TextField{Name: "source_type"})
	mustSaveDBCheckCollection(t, app, stacks)

	jobs := core.NewBaseCollection("scheduled_jobs")
	jobs.Fields.Add(&core.RelationField{Name: "repository", CollectionId: repos.Id, MaxSelect: 1})
	jobs.Fields.Add(&core.TextField{Name: "name"})
	jobs.Fields.Add(&core.TextField{Name: "description"})
	jobs.Fields.Add(&core.TextField{Name: "job_file"})
	jobs.Fields.Add(&core.BoolField{Name: "enabled"})
	jobs.Fields.Add(&core.TextField{Name: "status"})
	mustSaveDBCheckCollection(t, app, jobs)

	collection := core.NewBaseCollection("worker_tokens")
	collection.Fields.Add(&core.TextField{Name: "token_hash"})
	collection.Fields.Add(&core.TextField{Name: "status"})
	collection.Fields.Add(&core.RelationField{Name: "worker", CollectionId: workers.Id, MaxSelect: 1})
	mustSaveDBCheckCollection(t, app, collection)

	collection = core.NewBaseCollection("worker_commands")
	collection.Fields.Add(&core.RelationField{Name: "worker", CollectionId: workers.Id, MaxSelect: 1})
	collection.Fields.Add(&core.TextField{Name: "command_id"})
	collection.Fields.Add(&core.TextField{Name: "command_type"})
	collection.Fields.Add(&core.TextField{Name: "status"})
	mustSaveDBCheckCollection(t, app, collection)

	for _, name := range []string{"stack_env_vars", "stack_services", "stack_revisions", "stack_pending_reconciles", "sync_logs"} {
		collection = core.NewBaseCollection(name)
		collection.Fields.Add(&core.RelationField{Name: "stack", CollectionId: stacks.Id, MaxSelect: 1})
		collection.Fields.Add(&core.TextField{Name: "key"})
		collection.Fields.Add(&core.TextField{Name: "value"})
		collection.Fields.Add(&core.TextField{Name: "service_name"})
		collection.Fields.Add(&core.NumberField{Name: "version"})
		collection.Fields.Add(&core.TextField{Name: "commit_sha"})
		collection.Fields.Add(&core.TextField{Name: "checksum"})
		collection.Fields.Add(&core.TextField{Name: "compose_path"})
		collection.Fields.Add(&core.TextField{Name: "trigger"})
		collection.Fields.Add(&core.TextField{Name: "status"})
		mustSaveDBCheckCollection(t, app, collection)
	}

	collection = core.NewBaseCollection("job_env_vars")
	collection.Fields.Add(&core.RelationField{Name: "job", CollectionId: jobs.Id, MaxSelect: 1})
	collection.Fields.Add(&core.TextField{Name: "key"})
	collection.Fields.Add(&core.TextField{Name: "value"})
	mustSaveDBCheckCollection(t, app, collection)

	collection = core.NewBaseCollection("job_runs")
	collection.Fields.Add(&core.RelationField{Name: "job", CollectionId: jobs.Id, MaxSelect: 1})
	collection.Fields.Add(&core.RelationField{Name: "worker", CollectionId: workers.Id, MaxSelect: 1})
	collection.Fields.Add(&core.TextField{Name: "trigger"})
	collection.Fields.Add(&core.TextField{Name: "status"})
	mustSaveDBCheckCollection(t, app, collection)

	collection = core.NewBaseCollection("worker_policies")
	collection.Fields.Add(&core.BoolField{Name: "enabled"})
	mustSaveDBCheckCollection(t, app, collection)

	collection = core.NewBaseCollection("integrations")
	collection.Fields.Add(&core.TextField{Name: "slug"})
	collection.Fields.Add(&core.BoolField{Name: "enabled"})
	mustSaveDBCheckCollection(t, app, collection)

	collection = core.NewBaseCollection("invites")
	collection.Fields.Add(&core.TextField{Name: "email"})
	collection.Fields.Add(&core.TextField{Name: "token"})
	mustSaveDBCheckCollection(t, app, collection)
}

func mustSaveDBCheckCollection(t *testing.T, app core.App, col *core.Collection) {
	t.Helper()
	if err := app.Save(col); err != nil {
		t.Fatalf("failed to save collection %s: %v", col.Name, err)
	}
}

func mustCreateDBCheckRecord(t *testing.T, app core.App, collection string, fields map[string]any) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId(collection)
	if err != nil {
		t.Fatalf("failed to find collection %s: %v", collection, err)
	}
	rec := core.NewRecord(col)
	for key, value := range fields {
		rec.Set(key, value)
	}
	if err := app.Save(rec); err != nil {
		t.Fatalf("failed to save record in %s: %v", collection, err)
	}
	return rec
}

func assertIssue(t *testing.T, result Result, code, collection, field string) {
	t.Helper()
	for _, issue := range result.Issues {
		if issue.Code == code && issue.Collection == collection && issue.Field == field {
			return
		}
	}
	t.Fatalf("missing issue code=%s collection=%s field=%s in %#v", code, collection, field, result.Issues)
}
