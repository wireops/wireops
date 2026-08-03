package configfiles

import (
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func newTrackTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name"})
	mustSaveTrackCollection(t, app, stacks)

	jobs := core.NewBaseCollection("scheduled_jobs")
	jobs.Fields.Add(&core.TextField{Name: "name"})
	mustSaveTrackCollection(t, app, jobs)

	stackConfigFiles := core.NewBaseCollection("stack_config_files")
	stackConfigFiles.Fields.Add(&core.RelationField{Name: "stack", CollectionId: stacks.Id, Required: true, MaxSelect: 1})
	stackConfigFiles.Fields.Add(&core.TextField{Name: "name", Required: true})
	stackConfigFiles.Fields.Add(&core.TextField{Name: "source_path", Required: true})
	stackConfigFiles.Fields.Add(&core.TextField{Name: "target", Required: true})
	stackConfigFiles.Fields.Add(&core.TextField{Name: "checksum", Required: true})
	stackConfigFiles.AddIndex("idx_stack_config_files_unique", true, "stack, name", "")
	mustSaveTrackCollection(t, app, stackConfigFiles)

	jobConfigFiles := core.NewBaseCollection("job_config_files")
	jobConfigFiles.Fields.Add(&core.RelationField{Name: "job", CollectionId: jobs.Id, Required: true, MaxSelect: 1})
	jobConfigFiles.Fields.Add(&core.TextField{Name: "name", Required: true})
	jobConfigFiles.Fields.Add(&core.TextField{Name: "source_path", Required: true})
	jobConfigFiles.Fields.Add(&core.TextField{Name: "target", Required: true})
	jobConfigFiles.Fields.Add(&core.TextField{Name: "checksum", Required: true})
	jobConfigFiles.AddIndex("idx_job_config_files_unique", true, "job, name", "")
	mustSaveTrackCollection(t, app, jobConfigFiles)

	return app
}

func mustSaveTrackCollection(t *testing.T, app core.App, col *core.Collection) {
	t.Helper()
	if err := app.Save(col); err != nil {
		t.Fatalf("save collection %s: %v", col.Name, err)
	}
}

func mustCreateTrackRecord(t *testing.T, app core.App, collection string, values map[string]any) *core.Record {
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

func TestUpsertStackConfigFilesCreatesRows(t *testing.T) {
	app := newTrackTestApp(t)
	stack := mustCreateTrackRecord(t, app, "stacks", map[string]any{"name": "s1"})

	err := UpsertStackConfigFiles(app, stack.Id, []TrackedFile{
		{Name: "nginx-conf", SourcePath: "conf/nginx.conf", Target: "/etc/nginx/nginx.conf", Checksum: "abc123"},
	})
	if err != nil {
		t.Fatalf("UpsertStackConfigFiles: %v", err)
	}

	recs, err := app.FindAllRecords("stack_config_files", dbx.HashExp{"stack": stack.Id})
	if err != nil {
		t.Fatalf("find records: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if recs[0].GetString("name") != "nginx-conf" || recs[0].GetString("checksum") != "abc123" {
		t.Errorf("unexpected record: name=%q checksum=%q", recs[0].GetString("name"), recs[0].GetString("checksum"))
	}
}

func TestUpsertStackConfigFilesReplacesStaleRows(t *testing.T) {
	app := newTrackTestApp(t)
	stack := mustCreateTrackRecord(t, app, "stacks", map[string]any{"name": "s1"})

	if err := UpsertStackConfigFiles(app, stack.Id, []TrackedFile{
		{Name: "a", SourcePath: "a.conf", Target: "/a", Checksum: "1"},
		{Name: "b", SourcePath: "b.conf", Target: "/b", Checksum: "2"},
	}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	// Second render only declares "b" — "a" must be dropped (delete-then-recreate).
	if err := UpsertStackConfigFiles(app, stack.Id, []TrackedFile{
		{Name: "b", SourcePath: "b.conf", Target: "/b", Checksum: "3"},
	}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	recs, err := app.FindAllRecords("stack_config_files", dbx.HashExp{"stack": stack.Id})
	if err != nil {
		t.Fatalf("find records: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record after replace, got %d", len(recs))
	}
	if recs[0].GetString("name") != "b" || recs[0].GetString("checksum") != "3" {
		t.Errorf("unexpected surviving record: name=%q checksum=%q", recs[0].GetString("name"), recs[0].GetString("checksum"))
	}
}

func TestUpsertStackConfigFilesEmptyClearsRows(t *testing.T) {
	app := newTrackTestApp(t)
	stack := mustCreateTrackRecord(t, app, "stacks", map[string]any{"name": "s1"})

	if err := UpsertStackConfigFiles(app, stack.Id, []TrackedFile{
		{Name: "a", SourcePath: "a.conf", Target: "/a", Checksum: "1"},
	}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if err := UpsertStackConfigFiles(app, stack.Id, nil); err != nil {
		t.Fatalf("second upsert (empty): %v", err)
	}

	recs, err := app.FindAllRecords("stack_config_files", dbx.HashExp{"stack": stack.Id})
	if err != nil {
		t.Fatalf("find records: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected 0 records, got %d", len(recs))
	}
}

func TestUpsertStackConfigFilesRejectsDuplicateNameInSameStack(t *testing.T) {
	app := newTrackTestApp(t)
	stack := mustCreateTrackRecord(t, app, "stacks", map[string]any{"name": "s1"})

	if err := UpsertStackConfigFiles(app, stack.Id, []TrackedFile{
		{Name: "keep", SourcePath: "keep.conf", Target: "/keep", Checksum: "0"},
	}); err != nil {
		t.Fatalf("initial upsert: %v", err)
	}

	// Two rows sharing (stack, name) violate migration 65's unique index —
	// this is exactly what a caller must avoid (e.g. by service-qualifying
	// TrackedFile.Name when two services annotate the same friendly config
	// name), so the upsert must fail rather than silently keep one row.
	err := UpsertStackConfigFiles(app, stack.Id, []TrackedFile{
		{Name: "dup", SourcePath: "a.conf", Target: "/a", Checksum: "1"},
		{Name: "dup", SourcePath: "b.conf", Target: "/b", Checksum: "2"},
	})
	if err == nil {
		t.Fatal("expected unique constraint violation for duplicate (stack, name)")
	}

	// The transaction must roll back entirely: the failed second call must
	// not have deleted the row from the first call.
	recs, findErr := app.FindAllRecords("stack_config_files", dbx.HashExp{"stack": stack.Id})
	if findErr != nil {
		t.Fatalf("find records: %v", findErr)
	}
	if len(recs) != 1 || recs[0].GetString("name") != "keep" {
		t.Fatalf("expected rollback to leave the original 1 row untouched, got %d records: %+v", len(recs), recs)
	}
}

func TestUpsertJobConfigFilesCreatesRows(t *testing.T) {
	app := newTrackTestApp(t)
	job := mustCreateTrackRecord(t, app, "scheduled_jobs", map[string]any{"name": "j1"})

	err := UpsertJobConfigFiles(app, job.Id, []TrackedFile{
		{Name: "app-settings", SourcePath: "settings.json", Target: "/app/settings.json", Checksum: "xyz"},
	})
	if err != nil {
		t.Fatalf("UpsertJobConfigFiles: %v", err)
	}

	recs, err := app.FindAllRecords("job_config_files", dbx.HashExp{"job": job.Id})
	if err != nil {
		t.Fatalf("find records: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
}
