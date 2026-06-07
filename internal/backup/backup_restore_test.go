package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/archive"
)

func TestBackupArtifactCanRestoreWireopsData(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	defer app.Cleanup()
	ensureBackupRestoreCollections(t, app)

	repo := createBackupRestoreRecord(t, app, "repositories", map[string]any{
		"name":            "backup-repo",
		"git_url":         "https://example.com/wireops.git",
		"branch":          "main",
		"status":          "connected",
		"last_commit_sha": "before-backup",
		"platform":        "github",
	})
	job := createBackupRestoreRecord(t, app, "scheduled_jobs", map[string]any{
		"repository":  repo.Id,
		"name":        "Nightly Backup",
		"description": "Creates a database dump every night.",
		"job_file":    "ops/nightly-backup.yaml",
		"enabled":     true,
		"status":      "active",
	})

	jobFile := filepath.Join(app.DataDir(), "repositories", repo.Id, "ops", "nightly-backup.yaml")
	writeBackupRestoreFile(t, jobFile, "title: Nightly Backup\ncron: \"0 2 * * *\"\n")

	const backupName = "wireops_backup_restore_test.zip"
	if err := app.CreateBackup(context.Background(), backupName); err != nil {
		t.Fatalf("failed to create backup: %v", err)
	}

	extractedDir := t.TempDir()
	backupPath := filepath.Join(app.DataDir(), core.LocalBackupsDirName, backupName)
	if err := archive.Extract(backupPath, extractedDir); err != nil {
		t.Fatalf("failed to extract backup: %v", err)
	}

	if err := app.Delete(job); err != nil {
		t.Fatalf("failed to delete scheduled job before restore: %v", err)
	}
	repo.Set("last_commit_sha", "after-backup")
	if err := app.Save(repo); err != nil {
		t.Fatalf("failed to mutate repository before restore: %v", err)
	}
	writeBackupRestoreFile(t, jobFile, "title: Corrupted\n")

	restoredApp, err := tests.NewTestAppWithConfig(core.BaseAppConfig{
		DataDir:       extractedDir,
		EncryptionEnv: "pb_test_env",
	})
	if err != nil {
		t.Fatalf("failed to bootstrap app from extracted backup: %v", err)
	}
	defer restoredApp.Cleanup()

	restoredRepo, err := restoredApp.FindRecordById("repositories", repo.Id)
	if err != nil {
		t.Fatalf("failed to find restored repository: %v", err)
	}
	if got := restoredRepo.GetString("last_commit_sha"); got != "before-backup" {
		t.Fatalf("restored repository last_commit_sha = %q, want before-backup", got)
	}

	restoredJob, err := restoredApp.FindRecordById("scheduled_jobs", job.Id)
	if err != nil {
		t.Fatalf("failed to find restored scheduled job: %v", err)
	}
	if got := restoredJob.GetString("name"); got != "Nightly Backup" {
		t.Fatalf("restored scheduled job name = %q, want Nightly Backup", got)
	}
	if got := restoredJob.GetString("description"); got != "Creates a database dump every night." {
		t.Fatalf("restored scheduled job description = %q, want original description", got)
	}
	if got := restoredJob.GetString("status"); got != "active" {
		t.Fatalf("restored scheduled job status = %q, want active", got)
	}

	restoredFile, err := os.ReadFile(filepath.Join(restoredApp.DataDir(), "repositories", repo.Id, "ops", "nightly-backup.yaml"))
	if err != nil {
		t.Fatalf("failed to read restored job file: %v", err)
	}
	if got := string(restoredFile); got != "title: Nightly Backup\ncron: \"0 2 * * *\"\n" {
		t.Fatalf("restored job file = %q, want original content", got)
	}
}

func ensureBackupRestoreCollections(t *testing.T, app core.App) {
	t.Helper()

	repos := core.NewBaseCollection("repositories")
	repos.Fields.Add(&core.TextField{Name: "name", Required: true})
	repos.Fields.Add(&core.TextField{Name: "git_url", Required: true})
	repos.Fields.Add(&core.TextField{Name: "branch"})
	repos.Fields.Add(&core.SelectField{Name: "status", Values: []string{"connected", "error"}})
	repos.Fields.Add(&core.TextField{Name: "last_commit_sha"})
	repos.Fields.Add(&core.TextField{Name: "platform"})
	repos.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	repos.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
	if err := app.Save(repos); err != nil {
		t.Fatalf("failed to create repositories collection: %v", err)
	}

	jobs := core.NewBaseCollection("scheduled_jobs")
	jobs.Fields.Add(&core.RelationField{
		Name:         "repository",
		CollectionId: repos.Id,
		Required:     true,
		MaxSelect:    1,
	})
	jobs.Fields.Add(&core.TextField{Name: "name", Required: true, Pattern: `^[a-zA-Z0-9\p{L}_ -]+$`})
	jobs.Fields.Add(&core.TextField{Name: "description"})
	jobs.Fields.Add(&core.TextField{Name: "job_file", Required: true})
	jobs.Fields.Add(&core.BoolField{Name: "enabled"})
	jobs.Fields.Add(&core.SelectField{Name: "status", Values: []string{"active", "paused", "stalled", "error"}})
	jobs.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	jobs.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
	if err := app.Save(jobs); err != nil {
		t.Fatalf("failed to create scheduled_jobs collection: %v", err)
	}
}

func createBackupRestoreRecord(t *testing.T, app core.App, collection string, values map[string]any) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId(collection)
	if err != nil {
		t.Fatalf("failed to find collection %s: %v", collection, err)
	}
	rec := core.NewRecord(col)
	for key, value := range values {
		rec.Set(key, value)
	}
	if err := app.Save(rec); err != nil {
		t.Fatalf("failed to create %s record: %v", collection, err)
	}
	return rec
}

func writeBackupRestoreFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write data file: %v", err)
	}
}
