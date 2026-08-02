package jobscheduler

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketbase/dbx"

	"github.com/wireops/wireops/internal/job"
)

func TestResolveJobConfigFilesSingleFile(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	repo := createJobRepoRecord(t, app)
	jobRec := createScheduledJobRecord(t, app, repo.Id, "job.yaml")

	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "settings.json"), []byte(`{"a":1}`), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	s := &Scheduler{app: app, repoWorkspace: filepath.Dir(repoDir)}
	repoID := filepath.Base(repoDir)

	entries := []job.ConfigEntry{
		{Name: "settings", Path: "settings.json", Target: "/app/settings.json", Mode: "0400"},
	}

	specs, err := s.resolveJobConfigFiles(jobRec.Id, repoID, entries)
	if err != nil {
		t.Fatalf("resolveJobConfigFiles: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d: %+v", len(specs), specs)
	}
	got := specs[0]
	if got.Name != "settings" || got.Target != "/app/settings.json" || got.Mode != "0400" || got.RelPath != "" {
		t.Errorf("unexpected spec: %+v", got)
	}
	content, err := base64.StdEncoding.DecodeString(got.ContentB64)
	if err != nil || string(content) != `{"a":1}` {
		t.Errorf("unexpected content: %q err=%v", content, err)
	}

	recs, err := app.FindAllRecords("job_config_files", dbx.HashExp{"job": jobRec.Id})
	if err != nil {
		t.Fatalf("find tracking records: %v", err)
	}
	if len(recs) != 1 || recs[0].GetString("name") != "settings" || recs[0].GetString("target") != "/app/settings.json" {
		t.Errorf("unexpected tracking rows: %+v", recs)
	}
}

func TestResolveJobConfigFilesDirectoryGroup(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	repo := createJobRepoRecord(t, app)
	jobRec := createScheduledJobRecord(t, app, repo.Id, "job.yaml")

	repoDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoDir, "confd", "nested"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "confd", "a.conf"), []byte("A"), 0644); err != nil {
		t.Fatalf("write a.conf: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "confd", "nested", "b.conf"), []byte("B"), 0644); err != nil {
		t.Fatalf("write b.conf: %v", err)
	}
	s := &Scheduler{app: app, repoWorkspace: filepath.Dir(repoDir)}
	repoID := filepath.Base(repoDir)

	entries := []job.ConfigEntry{
		{Name: "confd", Path: "confd", Target: "/etc/app/conf.d"},
	}

	specs, err := s.resolveJobConfigFiles(jobRec.Id, repoID, entries)
	if err != nil {
		t.Fatalf("resolveJobConfigFiles: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs from directory expansion, got %d: %+v", len(specs), specs)
	}
	for _, spec := range specs {
		if spec.Name != "confd" || spec.Target != "/etc/app/conf.d" {
			t.Errorf("expected shared Name=confd Target=/etc/app/conf.d, got %+v", spec)
		}
		if spec.RelPath == "" {
			t.Errorf("expected non-empty RelPath for directory-expanded spec, got %+v", spec)
		}
	}

	recs, err := app.FindAllRecords("job_config_files", dbx.HashExp{"job": jobRec.Id})
	if err != nil {
		t.Fatalf("find tracking records: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 tracking rows, got %d: %+v", len(recs), recs)
	}
}

func TestResolveJobConfigFilesResolutionFailure(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	repo := createJobRepoRecord(t, app)
	jobRec := createScheduledJobRecord(t, app, repo.Id, "job.yaml")

	repoDir := t.TempDir()
	s := &Scheduler{app: app, repoWorkspace: filepath.Dir(repoDir)}
	repoID := filepath.Base(repoDir)

	entries := []job.ConfigEntry{
		{Name: "missing", Path: "does-not-exist.conf", Target: "/etc/missing.conf"},
	}

	specs, err := s.resolveJobConfigFiles(jobRec.Id, repoID, entries)
	if err == nil {
		t.Fatal("expected error for a config source that doesn't exist")
	}
	if specs != nil {
		t.Errorf("expected nil specs on resolution failure, got %+v", specs)
	}
}

func TestResolveJobConfigFilesEmptyEntries(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	repo := createJobRepoRecord(t, app)
	jobRec := createScheduledJobRecord(t, app, repo.Id, "job.yaml")

	s := &Scheduler{app: app, repoWorkspace: t.TempDir()}

	specs, err := s.resolveJobConfigFiles(jobRec.Id, repo.Id, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if specs != nil {
		t.Errorf("expected nil specs for no config entries, got %+v", specs)
	}
}

// TestResolveJobConfigFilesTrackingFailureStillReturnsSpecs verifies the
// best-effort tracking contract: resolveJobConfigFiles must still return the
// resolved specs (for RunJobCommand dispatch) even if the job_config_files
// upsert fails, since tracking is UI/drift visibility only, not a
// correctness dependency for the job actually running.
func TestResolveJobConfigFilesTrackingFailureStillReturnsSpecs(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	repo := createJobRepoRecord(t, app)
	jobRec := createScheduledJobRecord(t, app, repo.Id, "job.yaml")

	// Drop job_config_files so the tracking upsert inside
	// resolveJobConfigFiles fails (collection lookup error), without
	// affecting resolution itself.
	col, err := app.FindCollectionByNameOrId("job_config_files")
	if err != nil {
		t.Fatalf("find job_config_files collection: %v", err)
	}
	if err := app.Delete(col); err != nil {
		t.Fatalf("delete job_config_files collection: %v", err)
	}

	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "settings.json"), []byte("ok"), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	s := &Scheduler{app: app, repoWorkspace: filepath.Dir(repoDir)}
	repoID := filepath.Base(repoDir)

	entries := []job.ConfigEntry{
		{Name: "settings", Path: "settings.json", Target: "/app/settings.json"},
	}

	specs, err := s.resolveJobConfigFiles(jobRec.Id, repoID, entries)
	if err != nil {
		t.Fatalf("expected resolveJobConfigFiles to succeed despite tracking failure, got: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec despite tracking failure, got %d: %+v", len(specs), specs)
	}
}
